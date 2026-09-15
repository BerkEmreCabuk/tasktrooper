// Package urlguard vets outbound HTTP destinations that untrusted input chose.
//
// # Threat model
//
// This is an agent platform, and the LLM picks the URL. The model's
// context is full of text fetched from the open internet, so every outbound
// request whose destination comes from a tool argument, an agent-owned config
// row or an API request body is reachable by prompt injection and has to be
// treated as attacker-chosen: fetch_url, HTTP MCP endpoints, deploy health URLs
// and job callbacks all qualify.
//
// What an unguarded request is worth to an attacker:
//
//   - http://127.0.0.1:8080/... is this pod's own listener. Everything outside
//     /v1 and /admin is served without authentication (see
//     adapter/http/handler_ui.go), so loopback is a straight read of internal
//     endpoints from inside the trust boundary.
//   - http://169.254.169.254/... is the cloud metadata service. fetch_url on its
//     own cannot set the Metadata-Flavor header GCP demands, but an MCP server
//     config carries an arbitrary header map and can.
//   - 10/8, 172.16/12, 192.168/16, fc00::/7 and 100.64/10 are the operator's own
//     network: routers, NAS boxes and other services reachable from this
//     machine.
//   - Even without a reachable service, distinguishable transport errors
//     ("connection refused" versus "i/o timeout") turn any of the above into a
//     working port scanner whose output lands back in the model's context.
//     Callers must collapse every rejection and every transport failure into one
//     non-discriminating message; ErrBlocked exists so they can tell the two
//     apart internally while saying the same thing outward.
//
// The cluster NetworkPolicy cuts link-local, RFC1918 and the ClusterIP range at
// L3, but it cannot see a same-pod loopback connection at all, and it is applied
// but not positively verified in production. These checks are the reliable
// control, not a second layer behind one.
//
// # Why the dial is pinned
//
// Validating a hostname and then handing the URL to an ordinary http.Client is
// not a fix. The transport resolves the name a second time, and an attacker who
// controls the authoritative DNS for it answers a public address to the first
// lookup and 127.0.0.1 to the second — classic DNS rebinding, and the window is
// wide open because the two lookups are milliseconds apart by design.
//
// Client and ClientFor therefore vet the address inside Transport.DialContext
// and then connect to that literal IP, so the address that was checked is the
// address that is dialled with nothing in between. TLS is unaffected: net/http
// takes the SNI name and the certificate hostname from the request URL, never
// from the dialled address.
//
// Redirects get the same treatment. CheckRedirect re-runs the whole guard on
// every hop and caps the chain, because a public host answering 302 to
// http://127.0.0.1/ is the same attack with one extra step.
//
// # Loopback is opt-in, never implicit
//
// Self-hosted and desktop installs legitimately point tools at services on the
// same machine. That is a real product use, so it is an explicit policy switch
// (Policy.AllowLoopback) that is off in the zero value and off in PublicOnly.
// Default turns it on only when the operator sets ALLOW_LOOPBACK_TOOL_URLS: a
// process environment variable, never a database config row an agent (or a
// prompt injection) could reach.
package urlguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// AllowLoopbackEnv is the operator switch that re-enables loopback destinations
// for self-hosted installs. It is read once per Default call, from the process
// environment, which is deliberately not something an agent (or an
// attacker-controlled config row) can write to.
const AllowLoopbackEnv = "ALLOW_LOOPBACK_TOOL_URLS"

// defaultMaxRedirects caps a redirect chain. Five is well past what any real
// site needs and well short of Go's default ten, which is only a loop breaker.
const defaultMaxRedirects = 5

// ErrBlocked marks every rejection this package makes, so a caller can answer
// "blocked" and "the host was down" with the same words while still logging the
// difference. Match it with errors.Is; net/http wraps CheckRedirect and dial
// errors in *url.Error, which unwraps.
var ErrBlocked = errors.New("destination not permitted")

// Error is a single rejection. Its message names the host and the reason and is
// meant for logs only — never hand it to a model, that is the port scanner.
type Error struct {
	Reason string
	Host   string
	IP     net.IP
}

func (e *Error) Error() string {
	switch {
	case e.IP != nil && e.Host != "":
		return fmt.Sprintf("%s (%s): %s", e.Host, e.IP, e.Reason)
	case e.Host != "":
		return fmt.Sprintf("%s: %s", e.Host, e.Reason)
	default:
		return e.Reason
	}
}

// Is makes every rejection match ErrBlocked without each construction site
// having to remember to wrap it.
func (e *Error) Is(target error) bool { return target == ErrBlocked }

// Resolver is the slice of net.Resolver this package uses. It is an interface so
// tests can drive the rebinding case deterministically; production always ends
// up on net.DefaultResolver.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Policy is what a destination is judged against. The zero value is already
// restrictive (no schemes allowed at all), so a forgotten field fails closed;
// use PublicOnly or Default to get a working one.
type Policy struct {
	// Schemes is the URL scheme allowlist, lower-case. Nothing outside it is
	// dialled — file:, gopher: and data: are not "unusual", they are the
	// interesting half of an SSRF payload.
	Schemes []string

	// AllowLoopback permits 127.0.0.0/8, ::1 and the unspecified addresses.
	// Off unless a self-hosted operator asks for it; see the package comment.
	AllowLoopback bool

	// AllowPrivate permits RFC1918, IPv6 ULA and CGNAT. Nothing in this repo
	// sets it today; it exists so a future internal-only caller states the
	// exception in its policy instead of skipping the guard.
	AllowPrivate bool

	// MaxRedirects caps the hop count; <= 0 means defaultMaxRedirects.
	MaxRedirects int

	// Resolver overrides net.DefaultResolver. Tests only.
	Resolver Resolver

	// Dialer connects to an address this policy has already approved. Nil means
	// a plain net.Dialer. Tests only — it is below the guard, not around it.
	Dialer func(ctx context.Context, network, address string) (net.Conn, error)

	// TLSClientConfig overrides the transport's TLS settings, for an install
	// that needs a private CA in its roots. Nil means Go's defaults, which is
	// what production uses. It changes who is trusted, not where the connection
	// is allowed to go — the address rules run first either way.
	TLSClientConfig *tls.Config
}

// PublicOnly allows http and https to public unicast addresses and nothing
// else. This is the policy for anything an LLM or an API caller can steer.
func PublicOnly() Policy {
	return Policy{
		Schemes:      []string{"http", "https"},
		MaxRedirects: defaultMaxRedirects,
	}
}

// Default is PublicOnly with the self-hosted loopback exception applied from the
// environment. Call it at construction time, not per request: the answer is a
// deployment property and re-reading it per call only invites a mid-flight
// change.
func Default() Policy {
	p := PublicOnly()
	p.AllowLoopback = loopbackAllowed(os.Getenv)
	return p
}

// loopbackAllowed takes getenv as a parameter so the switch is testable without
// mutating the process environment, matching cmd/agent-server's env handling.
func loopbackAllowed(getenv func(string) string) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(getenv(AllowLoopbackEnv)))
	return err == nil && v
}

// Target is a destination that passed the full guard, including the addresses it
// resolved to. Hand it to ClientFor so the connection goes to one of exactly
// these IPs.
type Target struct {
	URL  *url.URL
	Host string
	IPs  []net.IP
}

// Precheck is the offline half of Validate: scheme, shape, and — when the host
// is written as a literal IP — the address rules. It performs no DNS.
//
// It is what write paths use. Resolving at write time would be both flaky (a
// health URL is routinely configured before the environment that answers it
// exists) and pointless as a security control, because the row is dialled
// minutes or days later and the answer can change in between. The dial is where
// the binding decision has to be made; this only stops the obvious
// http://127.0.0.1/ and http://169.254.169.254/ from ever being stored.
func (p Policy) Precheck(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, &Error{Reason: "not a valid URL"}
	}
	if !p.schemeAllowed(u.Scheme) {
		return nil, &Error{Reason: fmt.Sprintf("scheme %q is not allowed", u.Scheme)}
	}
	host := u.Hostname()
	if host == "" {
		return nil, &Error{Reason: "URL has no host"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if reason := p.blockedReason(ip); reason != "" {
			return nil, &Error{Host: host, IP: ip, Reason: reason}
		}
	}
	return u, nil
}

// Validate runs the whole guard: parse, scheme allowlist, resolve, and check
// every address the name answers with. Use ClientFor with the result so the
// connection cannot drift to an address this never saw.
func (p Policy) Validate(ctx context.Context, raw string) (*Target, error) {
	u, err := p.Precheck(raw)
	if err != nil {
		return nil, err
	}
	return p.ValidateURL(ctx, u)
}

// ValidateURL is Validate for an already-parsed URL; CheckRedirect uses it.
func (p Policy) ValidateURL(ctx context.Context, u *url.URL) (*Target, error) {
	if u == nil {
		return nil, &Error{Reason: "not a valid URL"}
	}
	if !p.schemeAllowed(u.Scheme) {
		return nil, &Error{Reason: fmt.Sprintf("scheme %q is not allowed", u.Scheme)}
	}
	host := u.Hostname()
	if host == "" {
		return nil, &Error{Reason: "URL has no host"}
	}
	ips, err := p.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	return &Target{URL: u, Host: host, IPs: ips}, nil
}

func (p Policy) schemeAllowed(scheme string) bool {
	scheme = strings.ToLower(scheme)
	for _, s := range p.Schemes {
		if scheme == strings.ToLower(s) {
			return true
		}
	}
	return false
}

func (p Policy) resolver() Resolver {
	if p.Resolver != nil {
		return p.Resolver
	}
	return net.DefaultResolver
}

// resolve turns a host into vetted addresses.
//
// Note what it does NOT do: filter. If any one of the answers is blocked the
// whole destination is refused, because the transport is free to pick any of
// them and an answer that mixes 93.184.216.34 with 127.0.0.1 is not a host with
// an unlucky address, it is the attack.
//
// Textual forms net.ParseIP rejects — 0177.0.0.1, 2130706433, 127.1, localhost,
// ::ffff:7f00:1 spelled as a name — need no special handling here precisely
// because the check is on the resolver's answer, not on the spelling. Whatever
// the platform resolver makes of them is what gets judged; a form it cannot
// resolve simply fails.
func (p Policy) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if reason := p.blockedReason(ip); reason != "" {
			return nil, &Error{Host: host, IP: ip, Reason: reason}
		}
		return []net.IP{ip}, nil
	}
	addrs, err := p.resolver().LookupIPAddr(ctx, host)
	if err != nil {
		return nil, &Error{Host: host, Reason: "host could not be resolved"}
	}
	if len(addrs) == 0 {
		return nil, &Error{Host: host, Reason: "host resolved to no addresses"}
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if reason := p.blockedReason(a.IP); reason != "" {
			return nil, &Error{Host: host, IP: a.IP, Reason: reason}
		}
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// Client returns an http.Client that re-runs the guard inside its dialer and on
// every redirect hop. Use it when the destination is fixed in code (a search
// provider's API) and there is no earlier Validate to pin to.
func (p Policy) Client(timeout time.Duration) *http.Client {
	return p.client(nil, timeout)
}

// ClientFor is Client with the first hop pinned to the addresses Validate
// already vetted for t, which is what closes the rebinding window: between the
// check and the connect there is no second lookup to poison.
func (p Policy) ClientFor(t *Target, timeout time.Duration) *http.Client {
	var pin map[string][]net.IP
	if t != nil && len(t.IPs) > 0 {
		pin = map[string][]net.IP{strings.ToLower(t.Host): t.IPs}
	}
	return p.client(pin, timeout)
}

func (p Policy) client(pin map[string][]net.IP, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// Proxy is nil on purpose, where http.DefaultTransport would honour
			// HTTP_PROXY. A proxy makes every check below meaningless: the
			// connection goes to the proxy, the guard approves the proxy, and
			// the destination the model actually chose is carried in a request
			// line this code never inspects. An operator who needs egress
			// through a proxy has to opt in by building their own transport,
			// having read this comment.
			Proxy:                 nil,
			DialContext:           p.dialContext(pin),
			TLSClientConfig:       p.TLSClientConfig,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          16,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: p.CheckRedirect,
	}
}

// CheckRedirect re-runs the full guard on each hop and caps the chain.
func (p Policy) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= p.maxRedirects() {
		return &Error{Reason: fmt.Sprintf("redirect chain longer than %d hops", p.maxRedirects())}
	}
	_, err := p.ValidateURL(req.Context(), req.URL)
	return err
}

// SameHostRedirect is CheckRedirect plus a refusal to leave endpoint's host.
//
// It exists for callers that attach a credential to the request — an MCP bearer
// token, a search provider's API key. net/http drops Authorization and Cookie
// across a host change, but only from the request it builds, and a header
// applied lower down (a RoundTripper) or under any other name (X-Api-Key,
// X-Subscription-Token) survives. Refusing the hop is the part that holds no
// matter where the header was set.
func (p Policy) SameHostRedirect(endpoint *url.URL) func(*http.Request, []*http.Request) error {
	var want string
	if endpoint != nil {
		want = strings.ToLower(endpoint.Host)
	}
	return func(req *http.Request, via []*http.Request) error {
		if want != "" && !strings.EqualFold(req.URL.Host, want) {
			return &Error{
				Host:   req.URL.Hostname(),
				Reason: fmt.Sprintf("redirect leaves the configured host %q", want),
			}
		}
		return p.CheckRedirect(req, via)
	}
}

func (p Policy) maxRedirects() int {
	if p.MaxRedirects > 0 {
		return p.MaxRedirects
	}
	return defaultMaxRedirects
}

// dialContext is the load-bearing half of this package. net/http hands it the
// host it is about to reach; it decides, and then connects to the exact address
// it decided about rather than handing the name back to the OS resolver.
func (p Policy) dialContext(pin map[string][]net.IP) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, &Error{Reason: "malformed dial address"}
		}
		ips, ok := pin[strings.ToLower(host)]
		if !ok {
			if ips, err = p.resolve(ctx, host); err != nil {
				return nil, err
			}
		}
		var lastErr error
		for _, ip := range ips {
			// Pinned addresses are re-checked too. A pin caches a decision this
			// policy made; it is not a way around it, and a caller that builds a
			// Target by hand must not be able to smuggle one past.
			if reason := p.blockedReason(ip); reason != "" {
				return nil, &Error{Host: host, IP: ip, Reason: reason}
			}
			conn, dialErr := p.baseDial(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = &Error{Host: host, Reason: "host resolved to no usable address"}
		}
		return nil, lastErr
	}
}

func (p Policy) baseDial(ctx context.Context, network, address string) (net.Conn, error) {
	if p.Dialer != nil {
		return p.Dialer(ctx, network, address)
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

// LogValue renders u for a log field: the password is redacted the way
// url.URL.Redacted does it, and the query string and fragment are dropped
// whole. Query strings on an agent-supplied URL routinely carry the caller's
// own tokens (a signed callback, a presigned object URL), and a log line is
// exactly the wrong place to keep them.
func LogValue(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.RawQuery = ""
	c.ForceQuery = false
	c.Fragment = ""
	c.RawFragment = ""
	return c.Redacted()
}

// LogRaw is LogValue for a string that may not parse. An unparseable URL is
// reduced to a fixed placeholder rather than logged verbatim.
func LogRaw(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "<unparseable url>"
	}
	return LogValue(u)
}
