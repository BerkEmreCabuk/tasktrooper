package urlguard_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// ─── test doubles ─────────────────────────────────────────────────────────────

// fakeResolver answers from a table and can give a different answer to each
// successive lookup of the same name — which is the whole of DNS rebinding.
type fakeResolver struct {
	mu    sync.Mutex
	names map[string][][]string
	calls map[string]int
}

func newResolver() *fakeResolver {
	return &fakeResolver{names: map[string][][]string{}, calls: map[string]int{}}
}

// sequence makes host answer answers[0] to the first lookup, answers[1] to the
// second, and the last entry to every lookup after that.
func (f *fakeResolver) sequence(host string, answers ...[]string) *fakeResolver {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names[strings.ToLower(host)] = answers
	return f
}

func (f *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	host = strings.ToLower(host)
	answers, ok := f.names[host]
	if !ok || len(answers) == 0 {
		return nil, fmt.Errorf("no such host: %s", host)
	}
	n := f.calls[host]
	f.calls[host]++
	if n >= len(answers) {
		n = len(answers) - 1
	}
	out := make([]net.IPAddr, 0, len(answers[n]))
	for _, raw := range answers[n] {
		ip := net.ParseIP(raw)
		if ip == nil {
			return nil, fmt.Errorf("test resolver: %q is not an IP", raw)
		}
		out = append(out, net.IPAddr{IP: ip})
	}
	return out, nil
}

func (f *fakeResolver) lookups(host string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[strings.ToLower(host)]
}

// recordingDialer records every address the guard decided to connect to, and
// routes approved connections to a real listener. Recording the decision — not
// the URL — is what lets a test assert which address was actually dialled.
type recordingDialer struct {
	mu     sync.Mutex
	routes map[string]string // vetted IP -> real listener address
	seen   []string
}

func (d *recordingDialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(address)
	d.mu.Lock()
	d.seen = append(d.seen, address)
	real, ok := d.routes[host]
	d.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("test dialer: nothing listening at %s", address)
	}
	var nd net.Dialer
	return nd.DialContext(ctx, network, real)
}

func (d *recordingDialer) addresses() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.seen...)
}

// harness wires a PublicOnly policy to a scripted resolver and a dialer that
// lands approved connections on httptest servers. Everything stays hermetic
// while the real transport, redirect and Host-header handling still run: from
// net/http's point of view it is talking to public.test over the network.
type harness struct {
	policy   urlguard.Policy
	resolver *fakeResolver
	dialer   *recordingDialer
}

func newHarness() *harness {
	h := &harness{resolver: newResolver(), dialer: &recordingDialer{routes: map[string]string{}}}
	h.policy = urlguard.PublicOnly()
	h.policy.Resolver = h.resolver
	h.policy.Dialer = h.dialer.dial
	return h
}

// host makes name resolve to ip; when srv is non-nil, connections to ip land on
// it. A nil srv models a name that resolves but has no listener.
func (h *harness) host(name, ip string, srv *httptest.Server) *harness {
	h.resolver.sequence(name, []string{ip})
	if srv != nil {
		h.dialer.routes[ip] = strings.TrimPrefix(srv.URL, "http://")
	}
	return h
}

// ─── address classes ──────────────────────────────────────────────────────────

// TestValidateRejectsInternalAddressClasses walks every range this guard exists
// to keep an LLM-chosen URL out of. Cases with no DNS answer are written as
// literals and short-circuit before the resolver; cases with one go through the
// resolver and are judged on what it returns.
func TestValidateRejectsInternalAddressClasses(t *testing.T) {
	cases := []struct {
		name    string
		host    string // as it appears in the URL
		answers []string
		want    string // substring of the rejection reason
	}{
		{"loopback v4 literal", "127.0.0.1", nil, "loopback"},
		{"loopback v4 whole /8", "127.99.12.3", nil, "loopback"},
		{"loopback v6 literal", "[::1]", nil, "loopback"},
		{"loopback via dns", "localhost.evil.test", []string{"127.0.0.1"}, "loopback"},
		{"link-local v4 metadata", "169.254.169.254", nil, "link-local"},
		{"link-local v4 via dns", "metadata.evil.test", []string{"169.254.169.254"}, "link-local"},
		{"link-local v6", "[fe80::1]", nil, "link-local"},
		{"rfc1918 10/8", "10.0.0.5", nil, "private"},
		{"rfc1918 172.16/12", "172.20.4.9", nil, "private"},
		{"rfc1918 192.168/16", "192.168.1.1", nil, "private"},
		{"rfc1918 via dns", "db.internal.test", []string{"10.4.0.12"}, "private"},
		{"ipv6 ula fd00::/8", "[fd00::1]", nil, "unique local"},
		{"ipv6 ula fc00::/8", "[fc00::abcd]", nil, "unique local"},
		{"v4-mapped v6 loopback", "[::ffff:127.0.0.1]", nil, "loopback"},
		{"v4-mapped v6 metadata", "[::ffff:169.254.169.254]", nil, "link-local"},
		{"v4-mapped v6 rfc1918", "[::ffff:10.1.2.3]", nil, "private"},
		{"unspecified v4", "0.0.0.0", nil, "unspecified"},
		{"unspecified v6", "[::]", nil, "unspecified"},
		{"cgnat 100.64/10", "100.64.0.1", nil, "carrier-grade"},
		{"cgnat upper edge", "100.127.255.254", nil, "carrier-grade"},
		{"cgnat via dns", "svc.cgnat.test", []string{"100.100.1.1"}, "carrier-grade"},
		{"multicast v4", "224.0.0.1", nil, "multicast"},
		{"multicast v6", "[ff02::1]", nil, "multicast"},
		{"broadcast", "255.255.255.255", nil, "reserved"},
		// Deprecated and transitional IPv6 encodings that carry a v4 address
		// but answer false to net.IP's own predicates.
		{"ipv4-compatible v6 loopback", "[::127.0.0.1]", nil, "loopback"},
		{"nat64 metadata", "[64:ff9b::169.254.169.254]", nil, "link-local"},
		{"6to4 rfc1918", "[2002:0a00:0001::1]", nil, "private"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := urlguard.PublicOnly()
			if tc.answers != nil {
				p.Resolver = newResolver().sequence(strings.Trim(tc.host, "[]"), tc.answers)
			}
			_, err := p.Validate(context.Background(), "http://"+tc.host+"/x")
			if err == nil {
				t.Fatalf("Validate(%s) allowed an internal destination", tc.host)
			}
			if !errors.Is(err, urlguard.ErrBlocked) {
				t.Fatalf("error %v does not match ErrBlocked", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("reason %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

// TestPrecheckRejectsLiteralsWithoutDNS proves the write-time half stops the
// obvious cases with no resolver in play: Policy.Resolver is nil here, so any
// lookup would have gone to the real one, and a name is deliberately let
// through to be judged at dial time instead.
func TestPrecheckRejectsLiteralsWithoutDNS(t *testing.T) {
	p := urlguard.PublicOnly()
	for _, raw := range []string{
		"http://127.0.0.1:8080/metrics",
		"http://169.254.169.254/computeMetadata/v1/",
		"http://10.0.0.1/",
		"http://[::1]:9000/",
		"http://[::ffff:127.0.0.1]/",
		"http://100.64.0.9/",
		"file:///etc/passwd",
		"http:///no-host",
	} {
		if _, err := p.Precheck(raw); !errors.Is(err, urlguard.ErrBlocked) {
			t.Fatalf("Precheck(%q) = %v, want ErrBlocked", raw, err)
		}
	}
	if _, err := p.Precheck("http://metadata.google.internal/"); err != nil {
		t.Fatalf("Precheck must not resolve names: %v", err)
	}
}

// TestValidateHostEncodings covers the spellings a probe reaches for. The point
// is that this package never decodes them itself: net.ParseIP rejects octal,
// decimal, hex and short forms, so they go to the resolver and the verdict is
// made on the answer — which is what a platform resolver returns for them.
func TestValidateHostEncodings(t *testing.T) {
	cases := []struct {
		name       string
		host       string
		parseIPOK  bool
		answers    []string // nil means the resolver has no entry for it
		wantReason string   // "" means the destination is allowed
	}{
		{"dotted quad", "127.0.0.1", true, nil, "loopback"},
		{"bracketed v6", "[::1]", true, nil, "loopback"},
		{"v4-mapped hex", "[::ffff:7f00:1]", true, nil, "loopback"},
		{"octal", "0177.0.0.1", false, []string{"127.0.0.1"}, "loopback"},
		{"decimal", "2130706433", false, []string{"127.0.0.1"}, "loopback"},
		{"short form", "127.1", false, []string{"127.0.0.1"}, "loopback"},
		{"hex", "0x7f000001", false, []string{"127.0.0.1"}, "loopback"},
		{"trailing dot", "localhost.", false, []string{"127.0.0.1"}, "loopback"},
		{"unresolvable name", "no-such-host.test", false, nil, "could not be resolved"},
		{"ordinary public host", "example.test", false, []string{"93.184.216.34"}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bare := strings.Trim(tc.host, "[]")
			if got := net.ParseIP(bare) != nil; got != tc.parseIPOK {
				t.Fatalf("net.ParseIP(%q) parsed = %v, this test assumes %v", bare, got, tc.parseIPOK)
			}
			p := urlguard.PublicOnly()
			r := newResolver()
			if tc.answers != nil {
				r.sequence(bare, tc.answers)
			}
			p.Resolver = r

			_, err := p.Validate(context.Background(), "http://"+tc.host+"/")
			if tc.wantReason == "" {
				if err != nil {
					t.Fatalf("a public host must be allowed, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantReason) {
				t.Fatalf("got %v, want a rejection mentioning %q", err, tc.wantReason)
			}
		})
	}
}

// TestValidateRejectsWholeAnswerWhenOneAddressIsInternal: the transport may pick
// any address a name returns, so a mixed answer is refused outright instead of
// being filtered down to the public one.
func TestValidateRejectsWholeAnswerWhenOneAddressIsInternal(t *testing.T) {
	p := urlguard.PublicOnly()
	p.Resolver = newResolver().sequence("split.test", []string{"93.184.216.34", "127.0.0.1"})
	if _, err := p.Validate(context.Background(), "http://split.test/"); !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("a mixed DNS answer must be refused, got %v", err)
	}
}

func TestValidateSchemeAllowlist(t *testing.T) {
	p := urlguard.PublicOnly()
	p.Resolver = newResolver().sequence("example.test", []string{"93.184.216.34"})
	for _, raw := range []string{
		"file:///etc/passwd",
		"gopher://example.test/",
		"ftp://example.test/",
		"data:text/plain,hello",
		"http+unix://example.test/",
	} {
		if _, err := p.Validate(context.Background(), raw); !errors.Is(err, urlguard.ErrBlocked) {
			t.Fatalf("Validate(%q) = %v, want ErrBlocked", raw, err)
		}
	}
	for _, raw := range []string{"http://example.test/", "HTTPS://example.test/"} {
		if _, err := p.Validate(context.Background(), raw); err != nil {
			t.Fatalf("Validate(%q) must be allowed: %v", raw, err)
		}
	}
}

// ─── loopback is opt-in ───────────────────────────────────────────────────────

func TestAllowLoopbackIsExplicit(t *testing.T) {
	blocked := urlguard.PublicOnly()
	if _, err := blocked.Validate(context.Background(), "http://127.0.0.1:8080/"); err == nil {
		t.Fatal("loopback must be off in PublicOnly")
	}

	// Self-hosted: the operator flips the switch and local services work again.
	allowed := urlguard.PublicOnly()
	allowed.AllowLoopback = true
	for _, raw := range []string{"http://127.0.0.1:8080/", "http://[::1]:8080/", "http://0.0.0.0:8080/"} {
		if _, err := allowed.Validate(context.Background(), raw); err != nil {
			t.Fatalf("AllowLoopback must permit %q: %v", raw, err)
		}
	}
	// It opens loopback and nothing else: the metadata service and the cluster
	// stay shut even for a self-hosted install, because nothing local is there.
	for _, raw := range []string{"http://169.254.169.254/", "http://10.0.0.1/", "http://100.64.0.1/"} {
		if _, err := allowed.Validate(context.Background(), raw); !errors.Is(err, urlguard.ErrBlocked) {
			t.Fatalf("AllowLoopback must not open %q: %v", raw, err)
		}
	}
}

func TestZeroPolicyFailsClosed(t *testing.T) {
	var zero urlguard.Policy
	if _, err := zero.Validate(context.Background(), "http://example.test/"); err == nil {
		t.Fatal("the zero Policy must allow no scheme at all")
	}
}

// ─── redirects ────────────────────────────────────────────────────────────────

// TestRedirectToInternalIsRefused is the second-hop version of the attack: the
// first host is genuinely public and answers 302 to somewhere it must not be
// able to send us.
func TestRedirectToInternalIsRefused(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1:8080/metrics",
		"http://169.254.169.254/computeMetadata/v1/instance/service-accounts/",
		"http://10.0.0.5/",
		"http://internal.test/",
	} {
		t.Run(target, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer srv.Close()

			h := newHarness().host("public.test", "93.184.216.34", srv)
			h.resolver.sequence("internal.test", []string{"10.9.9.9"})

			_, err := h.policy.Client(5 * time.Second).Get("http://public.test/")
			if !errors.Is(err, urlguard.ErrBlocked) {
				t.Fatalf("redirect to %s was not blocked: %v", target, err)
			}
			for _, addr := range h.dialer.addresses() {
				if addr != "93.184.216.34:80" {
					t.Fatalf("the guard dialled %s on the way to %s", addr, target)
				}
			}
		})
	}
}

func TestRedirectChainIsCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://public.test"+r.URL.Path+"x", http.StatusFound)
	}))
	defer srv.Close()

	h := newHarness().host("public.test", "93.184.216.34", srv)
	h.policy.MaxRedirects = 3

	_, err := h.policy.Client(5 * time.Second).Get("http://public.test/")
	if !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("an unbounded redirect loop must be cut, got %v", err)
	}
	if !strings.Contains(err.Error(), "3 hops") {
		t.Fatalf("the error should name the cap, got %v", err)
	}
}

func TestSameHostRedirectRefusesToLeaveTheConfiguredHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://lookalike.test/steal", http.StatusFound)
	}))
	defer srv.Close()

	h := newHarness().host("public.test", "93.184.216.34", srv)
	h.host("lookalike.test", "198.51.100.7", srv)

	endpoint, _ := url.Parse("http://public.test/mcp")
	client := h.policy.Client(5 * time.Second)
	client.CheckRedirect = h.policy.SameHostRedirect(endpoint)

	_, err := client.Get(endpoint.String())
	if !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("a cross-host redirect must be refused even when the target is public, got %v", err)
	}
	if !strings.Contains(err.Error(), "public.test") {
		t.Fatalf("the error should name the configured host, got %v", err)
	}
}

// ─── DNS rebinding ────────────────────────────────────────────────────────────

// TestDNSRebindingBetweenValidateAndDial is the reason the dial lives in this
// package. The name answers public to the first lookup — the one Validate makes
// — and loopback to every one after it.
func TestDNSRebindingBetweenValidateAndDial(t *testing.T) {
	h := newHarness()
	h.resolver.sequence("rebind.test",
		[]string{"93.184.216.34"}, // what Validate sees
		[]string{"127.0.0.1"},     // what an unguarded transport would then dial
	)

	target, err := h.policy.Validate(context.Background(), "http://rebind.test/")
	if err != nil {
		t.Fatalf("the first answer is public and must pass: %v", err)
	}
	if got := target.IPs[0].String(); got != "93.184.216.34" {
		t.Fatalf("vetted IP = %s", got)
	}

	// An unpinned client re-resolves at dial time and catches the flip there —
	// the case a validate-only guard misses completely.
	if _, err := h.policy.Client(5 * time.Second).Get("http://rebind.test/"); !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("the rebound address must be refused at dial time, got %v", err)
	}
	for _, addr := range h.dialer.addresses() {
		if strings.HasPrefix(addr, "127.0.0.1:") {
			t.Fatalf("the guard connected to the rebound address %s", addr)
		}
	}

	// A pinned client never asks again, so there is no second answer to poison.
	before := h.resolver.lookups("rebind.test")
	_, _ = h.policy.ClientFor(target, 5*time.Second).Get("http://rebind.test/")
	if after := h.resolver.lookups("rebind.test"); after != before {
		t.Fatalf("ClientFor must not re-resolve a pinned host (%d -> %d lookups)", before, after)
	}
	addrs := h.dialer.addresses()
	if last := addrs[len(addrs)-1]; last != "93.184.216.34:80" {
		t.Fatalf("pinned dial went to %s, want the vetted address", last)
	}
}

// TestPinnedTargetCannotSmuggleABlockedAddress: a Target caches a decision this
// policy made; it is not a way around it.
func TestPinnedTargetCannotSmuggleABlockedAddress(t *testing.T) {
	h := newHarness()
	forged := &urlguard.Target{
		URL:  mustParse(t, "http://rebind.test/"),
		Host: "rebind.test",
		IPs:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	if _, err := h.policy.ClientFor(forged, 5*time.Second).Get("http://rebind.test/"); !errors.Is(err, urlguard.ErrBlocked) {
		t.Fatalf("a pinned loopback address must still be refused, got %v", err)
	}
	if got := h.dialer.addresses(); len(got) != 0 {
		t.Fatalf("nothing should have been dialled, got %v", got)
	}
}

// ─── the product still works ──────────────────────────────────────────────────

func TestOrdinaryPublicFetchStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "public.test" {
			t.Errorf("Host header = %q, want the URL host rather than the dialled IP", r.Host)
		}
		_, _ = w.Write([]byte("hello from the internet"))
	}))
	defer srv.Close()

	h := newHarness().host("public.test", "93.184.216.34", srv)
	resp, err := h.policy.Client(5 * time.Second).Get("http://public.test/page?token=secret")
	if err != nil {
		t.Fatalf("a public URL must still be fetchable: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := h.dialer.addresses(); len(got) == 0 || got[0] != "93.184.216.34:80" {
		t.Fatalf("dialled %v, want the vetted address", got)
	}
}

func TestFollowsARedirectToAnotherPublicHost(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("final"))
	}))
	defer final.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://elsewhere.test/", http.StatusFound)
	}))
	defer first.Close()

	h := newHarness().host("public.test", "93.184.216.34", first)
	h.host("elsewhere.test", "198.51.100.7", final)

	resp, err := h.policy.Client(5 * time.Second).Get("http://public.test/")
	if err != nil {
		t.Fatalf("a public -> public redirect must still be followed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// ─── logging ──────────────────────────────────────────────────────────────────

func TestLogValueDropsSecrets(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://api.test/v1/x?token=s3cret&a=b", "https://api.test/v1/x"},
		{"https://user:hunter2@api.test/x", "https://user:xxxxx@api.test/x"},
		{"https://api.test/x#frag", "https://api.test/x"},
		{"https://api.test/x?", "https://api.test/x"},
	}
	for _, tc := range cases {
		if got := urlguard.LogRaw(tc.in); got != tc.want {
			t.Fatalf("LogRaw(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := urlguard.LogValue(nil); got != "" {
		t.Fatalf("LogValue(nil) = %q", got)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	return u
}
