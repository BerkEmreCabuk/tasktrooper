package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// localPolicy is the self-hosted shape: loopback reachable (so httptest works),
// everything else internal still shut.
func localPolicy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = true
	return p
}

// ─── F4: the bearer token must not follow a redirect ──────────────────────────

// TestConfiguredHeadersNeverFollowACrossHostRedirect is the finding itself.
//
// The MCP host answers 302 to an attacker's host. Go's http.Client would have
// stripped Authorization from the request it builds, but headerTransport sat
// below that and re-applied every configured header unconditionally, and
// nothing capped or checked the redirect — so the configured bearer token was
// replayed to the attacker in cleartext.
func TestConfiguredHeadersNeverFollowACrossHostRedirect(t *testing.T) {
	var mu sync.Mutex
	var stolen []string
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stolen = append(stolen, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()

	mcpHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/steal", http.StatusFound)
	}))
	defer mcpHost.Close()

	cfg := domain.MCPServerConfig{
		ID:        "evil",
		Transport: "http",
		URL:       mcpHost.URL + "/mcp",
		Headers:   map[string]string{"Authorization": "Bearer tenant-secret"},
	}

	client, err := guardedMCPClient(context.Background(), cfg, localPolicy())
	if err != nil {
		t.Fatalf("a loopback MCP endpoint must be reachable under the self-hosted policy: %v", err)
	}

	resp, err := client.Get(cfg.URL)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("the cross-host redirect was followed")
	}
	if !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, got := range stolen {
		if got != "" {
			t.Fatalf("the tenant bearer token reached the attacker: %q", got)
		}
	}
	if len(stolen) != 0 {
		t.Fatalf("the attacker host was contacted at all (%d times)", len(stolen))
	}
}

// TestHeaderTransportRefusesAForeignHost covers the RoundTripper on its own: it
// cannot see net/http's own stripping decision, so the only safe thing it can do
// is refuse to re-attach a credential to a host it does not belong to.
func TestHeaderTransportRefusesAForeignHost(t *testing.T) {
	var applied http.Header
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		applied = req.Header.Clone()
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
	})
	rt := &headerTransport{
		base:    base,
		headers: map[string]string{"Authorization": "Bearer tenant-secret"},
		host:    "mcp.example.test",
	}

	same, _ := http.NewRequest(http.MethodGet, "https://mcp.example.test/mcp", nil)
	if _, err := rt.RoundTrip(same); err != nil {
		t.Fatalf("the configured host must still get its headers: %v", err)
	}
	if applied.Get("Authorization") != "Bearer tenant-secret" {
		t.Fatal("the configured host did not get its headers")
	}

	applied = nil
	other, _ := http.NewRequest(http.MethodGet, "https://lookalike.test/mcp", nil)
	if _, err := rt.RoundTrip(other); err == nil {
		t.Fatal("headers were applied to a foreign host")
	}
	if applied != nil {
		t.Fatal("the request reached the transport below at all")
	}
}

// ─── F5: the endpoint is re-checked at connect time ───────────────────────────

// TestConnectTimeRefusesInternalEndpoints: a write-time check only covers rows
// written after it shipped. Every stored row is judged again here, where the
// header map that satisfies GCP's Metadata-Flavor gate would actually be sent.
func TestConnectTimeRefusesInternalEndpoints(t *testing.T) {
	cases := []string{
		"http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://10.4.0.9/mcp",
		"http://127.0.0.1:8080/mcp",
		"http://[::1]:8080/mcp",
		"http://100.64.0.1/mcp",
		"file:///etc/passwd",
	}
	policy := urlguard.PublicOnly() // production: loopback off
	policy.Resolver = staticResolver{"metadata.google.internal": "169.254.169.254"}

	for _, raw := range cases {
		cfg := domain.MCPServerConfig{
			ID:        "evil",
			Transport: "http",
			URL:       raw,
			Headers:   map[string]string{"Metadata-Flavor": "Google"},
		}
		if _, err := guardedMCPClient(context.Background(), cfg, policy); err == nil {
			t.Fatalf("connect to %s was allowed", raw)
		} else if !errors.Is(err, urlguard.ErrBlocked) {
			t.Fatalf("%s: want ErrBlocked, got %v", raw, err)
		}
	}
}

func TestConnectTimeAllowsAnOrdinaryEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tenant-secret" {
			t.Errorf("Authorization = %q, the configured header must still be sent", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := domain.MCPServerConfig{
		ID:        "ok",
		Transport: "http",
		URL:       srv.URL + "/mcp",
		Headers:   map[string]string{"Authorization": "Bearer tenant-secret"},
	}
	client, err := guardedMCPClient(context.Background(), cfg, localPolicy())
	if err != nil {
		t.Fatalf("a legitimate endpoint must connect: %v", err)
	}
	resp, err := client.Get(cfg.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// TestZeroManagerUsesTheDefaultPolicy: runtime builds &Manager{}, and a zero
// urlguard.Policy allows no scheme at all, so the lazy default is what keeps
// every HTTP MCP server from failing to connect.
func TestZeroManagerUsesTheDefaultPolicy(t *testing.T) {
	var m Manager
	if got := m.urlPolicy().Schemes; len(got) == 0 {
		t.Fatal("a zero Manager must fall back to urlguard.Default")
	}
	m.SetURLPolicy(localPolicy())
	if !m.urlPolicy().AllowLoopback {
		t.Fatal("SetURLPolicy did not take effect")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// staticResolver maps a name to one address, so the metadata-by-name case does
// not depend on what the machine running the test resolves.
type staticResolver map[string]string

func (s staticResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ip, ok := s[strings.ToLower(host)]
	if !ok {
		return nil, errors.New("no such host")
	}
	return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
}
