package search_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/search"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// The backend endpoints are constants inside the package, so the only way to
// point them at a test server is to control what their names resolve to and
// where an approved address dials. That is also the closest thing to the real
// threat: a poisoned answer, or a search backend that has been taken over.
type harness struct {
	mu     sync.Mutex
	names  map[string]string // hostname -> address the guard will vet
	routes map[string]string // vetted IP -> real listener
	dials  map[string]int    // vetted IP -> times it was dialled
}

func newHarness() *harness {
	return &harness{names: map[string]string{}, routes: map[string]string{}, dials: map[string]int{}}
}

func (h *harness) serve(hostname, ip string, srv *httptest.Server) *harness {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.names[strings.ToLower(hostname)] = ip
	if srv != nil {
		h.routes[ip] = srv.Listener.Addr().String()
	}
	return h
}

func (h *harness) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ip, ok := h.names[strings.ToLower(host)]
	if !ok {
		return nil, fmt.Errorf("no such host: %s", host)
	}
	return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
}

// dialsTo counts attempts, not connections: a backend whose listener is gone
// still shows up here, which is the only way to see how many times a refused
// connection was tried.
func (h *harness) dialsTo(ip string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dials[ip]
}

func (h *harness) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(address)
	h.mu.Lock()
	h.dials[host]++
	real, ok := h.routes[host]
	h.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("nothing listening at %s", address)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, real)
}

func (h *harness) policy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.Resolver = h
	p.Dialer = h.dial
	// The endpoints are https, so the test listeners have to be too. Their
	// certificates are httptest's self-signed pair issued for example.com, and
	// the request names them html.duckduckgo.com — verification is off because
	// what is under test is the redirect and body handling, not TLS.
	p.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test listeners
	return p
}

const (
	ddgIP  = "93.184.216.34"
	bingIP = "93.184.216.35"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

// page serves a fixture and counts how many times it was asked, recording each
// request so a test can assert on method, headers and body.
type page struct {
	hits     atomic.Int64
	requests syncSlice
	agents   syncSlice
	body     string
	status   int
}

func (p *page) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.hits.Add(1)
		p.agents.add(r.Header.Get("User-Agent"))
		if r.Method == http.MethodPost {
			b := make([]byte, 512)
			n, _ := r.Body.Read(b)
			p.requests.add(r.Method + " " + r.URL.RequestURI() + " " + string(b[:n]))
		} else {
			p.requests.add(r.Method + " " + r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if p.status != 0 {
			w.WriteHeader(p.status)
		}
		_, _ = w.Write([]byte(p.body))
	}
}

// ─── the two backends, and when the second one is used ────────────────────────

// TestDuckDuckGoAnswersAndBingIsNotAsked is the happy path. Bing is a fallback,
// not a second opinion: asking it anyway would double the outbound traffic and
// double the chance of being walled.
func TestDuckDuckGoAnswersAndBingIsNotAsked(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-html-post.html")}
	bing := &page{body: fixture(t, "bing-html.html")}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"golang fiber websocket"}`)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "source: duckduckgo") {
		t.Errorf("content does not name its source:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "https://docs.gofiber.io/contrib/websocket/") {
		t.Errorf("content does not carry the first result:\n%s", result.Content)
	}
	if n := bing.hits.Load(); n != 0 {
		t.Errorf("Bing was asked %d times while DuckDuckGo was answering", n)
	}
	if n := ddg.hits.Load(); n != 1 {
		t.Errorf("DuckDuckGo was asked %d times, want 1", n)
	}

	// The POST form and the browser headers are the whole reason this endpoint
	// answers at all — a GET gets the anomaly page.
	req := ddg.requests.all()[0]
	if !strings.HasPrefix(req, "POST /html/ ") {
		t.Errorf("unexpected request line: %q", req)
	}
	for _, want := range []string{"q=golang+fiber+websocket", "&b=", "kl=wt-wt"} {
		if !strings.Contains(req, want) {
			t.Errorf("the form body is missing %q: %q", want, req)
		}
	}
	if ua := ddg.agents.all()[0]; !strings.Contains(ua, "Mozilla/5.0") || !strings.Contains(ua, "Chrome/") {
		t.Errorf("the request did not look like a browser: %q", ua)
	}
}

// TestBotWalledDuckDuckGoFallsBackToBing is the finding this rewrite is built
// on: html.duckduckgo.com answers 202 with a challenge page, not an error, so a
// status check alone would have called it a success and returned nothing.
func TestBotWalledDuckDuckGoFallsBackToBing(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-anomaly-202.html"), status: http.StatusAccepted}
	bing := &page{body: fixture(t, "bing-html.html")}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"google translate"}`)

	if result.IsError {
		t.Fatalf("the fallback did not answer: %s", result.Content)
	}
	if !strings.Contains(result.Content, "source: bing") {
		t.Errorf("the answer is not attributed to Bing:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "https://support.google.com/translate/") {
		t.Errorf("the Bing results did not survive parsing:\n%s", result.Content)
	}
	if n := ddg.hits.Load(); n != 2 {
		t.Errorf("DuckDuckGo was asked %d times, want 2 (one retry)", n)
	}
	if n := bing.hits.Load(); n != 1 {
		t.Errorf("Bing was asked %d times, want 1", n)
	}
	if agents := ddg.agents.all(); len(agents) == 2 && agents[0] == agents[1] {
		t.Errorf("the retry reused the same user agent: %q", agents[0])
	}
	if !strings.Contains(bing.requests.all()[0], "GET /search?q=google+translate") {
		t.Errorf("unexpected Bing request: %q", bing.requests.all()[0])
	}
}

// A 200 that carries a challenge instead of results is the same wall wearing a
// different status.
func TestChallengePageOnA200AlsoFallsBack(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-anomaly-202.html")}
	bing := &page{body: fixture(t, "bing-html.html")}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if result.IsError || !strings.Contains(result.Content, "source: bing") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

// TestBothBackendsDownSaysWhatToDoInstead. "search error: EOF" reads to a model
// like bad luck worth retrying; one production run spent three iterations on
// exactly that. The refusal names both backends and the tool to reach for next.
func TestBothBackendsDownSaysWhatToDoInstead(t *testing.T) {
	down := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, down).
		serve("www.bing.com", bingIP, down)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if !result.IsError {
		t.Fatalf("two dead backends answered as a success: %+v", result)
	}
	for _, want := range []string{
		"web search backends unavailable right now",
		"duckduckgo:",
		"bing:",
		"503",
		"fetch_url",
	} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("the refusal does not mention %q: %q", want, result.Content)
		}
	}
}

// ─── the redirect must not leave the backend's host ───────────────────────────

// TestRedirectToAnotherHostIsRefused. There is no API key to leak any more, but
// a backend that can redirect this process anywhere is a request-forgery
// primitive on its own — the pod reaches hosts of the redirector's choosing with
// the pod's own network position.
func TestRedirectToAnotherHostIsRefused(t *testing.T) {
	var contacted atomic.Int64
	attacker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		contacted.Add(1)
		_, _ = w.Write([]byte("collected"))
	}))
	defer attacker.Close()

	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.test/collect", http.StatusFound)
	}))
	defer redirector.Close()

	down := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, redirector).
		serve("www.bing.com", bingIP, down).
		serve("evil.test", "198.51.100.7", attacker)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if !result.IsError {
		t.Fatalf("the redirect was followed to completion: %+v", result)
	}
	if n := contacted.Load(); n != 0 {
		t.Fatalf("the attacker host was contacted %d times", n)
	}
}

// A same-host redirect — the ordinary http -> https or path-move case — is still
// followed, so the fix does not break a backend that uses one.
func TestSameHostRedirectIsStillFollowed(t *testing.T) {
	body := fixture(t, "ddg-html-post.html")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/moved") {
			http.Redirect(w, r, r.URL.Path+"moved", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	h := newHarness().serve("html.duckduckgo.com", ddgIP, srv)
	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if result.IsError {
		t.Fatalf("a same-host redirect must still work: %s", result.Content)
	}
	if !strings.Contains(result.Content, "https://docs.gofiber.io/contrib/websocket/") {
		t.Fatalf("unexpected content: %s", result.Content)
	}
}

// ─── the body is bounded ──────────────────────────────────────────────────────

// TestResponsesAreBounded: a backend that streams forever must not take the pod
// with it. 512 KB is far past any real result page.
func TestResponsesAreBounded(t *testing.T) {
	// Counted per request rather than in total: the number of attempts is a
	// separate decision (one retry per backend, two backends), and folding it
	// into this bound would make the test fail for the wrong reason the day it
	// changes.
	var deepest atomic.Int64
	flood := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		chunk := strings.Repeat("A", 64*1024)
		var written int64
		// 32 MB on offer. With the cap in place the client stops reading at
		// 512 KB and these writes start failing long before the loop ends.
		for i := 0; i < 512; i++ {
			n, err := w.Write([]byte(chunk))
			written += int64(n)
			if err != nil {
				break
			}
		}
		for {
			prev := deepest.Load()
			if written <= prev || deepest.CompareAndSwap(prev, written) {
				break
			}
		}
	}))
	defer flood.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, flood).
		serve("www.bing.com", bingIP, flood)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if !result.IsError {
		t.Fatalf("an endless page answered as a success: %+v", result)
	}
	if got := deepest.Load(); got > 8*1024*1024 {
		t.Fatalf("one response pushed %d bytes into the reader; the limit is not in effect", got)
	}
}

// ─── the guard applies to the backend hosts too ───────────────────────────────

// TestBackendHostResolvingInternalIsRefused: the endpoints are constants, but
// what their names resolve to is not something this pod gets to trust.
func TestBackendHostResolvingInternalIsRefused(t *testing.T) {
	h := newHarness().
		serve("html.duckduckgo.com", "127.0.0.1", nil).
		serve("www.bing.com", "169.254.169.254", nil)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if !result.IsError {
		t.Fatal("a backend host resolving to loopback was dialled")
	}
	// What the guard knows is where a name pointed. Handing that to the model
	// publishes the pod's view of the network — one query at a time, a port
	// scan — so the refusal says only that a policy refused it.
	if !strings.Contains(result.Content, "blocked by the outbound URL policy") {
		t.Errorf("the refusal does not name the policy: %q", result.Content)
	}
	for _, leak := range []string{"127.0.0.1", "169.254.169.254", "loopback", "link-local"} {
		if strings.Contains(result.Content, leak) {
			t.Errorf("the refusal disclosed %q: %q", leak, result.Content)
		}
	}
}

// ─── politeness and the cache ─────────────────────────────────────────────────

// TestOutboundRequestsAreSpacedApart pins both the 1s gap and the retry budget:
// three requests (two at DuckDuckGo, one at Bing) means exactly two waits, and
// no fourth request means one retry per backend and no more.
func TestOutboundRequestsAreSpacedApart(t *testing.T) {
	down := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()
	bing := &page{body: fixture(t, "bing-html.html")}
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, down).
		serve("www.bing.com", bingIP, bingSrv)

	// A clock that moves only when the code sleeps, so the gap is asserted
	// without spending it. It has to move: the limiter reserves each slot up
	// front, so against a wholly frozen clock the reservations stack and the
	// second wait would read 2s — an artefact of the fake clock, not of the
	// spacing under test.
	clock := &fakeClock{now: time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)}
	var slept syncDurations
	tl := search.New(5,
		search.WithURLPolicy(h.policy()),
		search.WithClock(
			clock.Now,
			func(_ context.Context, d time.Duration) error { slept.add(d); clock.advance(d); return nil },
		),
	)

	result := tl.Execute(context.Background(), `{"query":"anything"}`)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	waits := slept.all()
	if len(waits) != 2 {
		t.Fatalf("waited %d times for 3 requests: %v", len(waits), waits)
	}
	for _, d := range waits {
		if d != time.Second {
			t.Errorf("wait of %s, want 1s", d)
		}
	}
	if n := bing.hits.Load(); n != 1 {
		t.Errorf("Bing was asked %d times, want 1", n)
	}
}

// TestRepeatedQueryIsServedFromCache. An agent loop asks the same question
// twice more often than one would think — a retry after a bad fetch, a second
// pass over the same plan — and each repeat is another chance to be walled.
func TestRepeatedQueryIsServedFromCache(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-html-post.html")}
	srv := httptest.NewTLSServer(ddg.handler())
	defer srv.Close()

	h := newHarness().serve("html.duckduckgo.com", ddgIP, srv)
	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))

	first := tl.Execute(context.Background(), `{"query":"golang fiber websocket"}`)
	if first.IsError {
		t.Fatalf("unexpected error: %s", first.Content)
	}
	// Different spelling, same query: the key is normalised.
	second := tl.Execute(context.Background(), `{"query":"  Golang   Fiber   WebSocket "}`)

	if second.Content != first.Content {
		t.Errorf("the cached answer differs:\n%s\n---\n%s", first.Content, second.Content)
	}
	if n := ddg.hits.Load(); n != 1 {
		t.Errorf("the backend was asked %d times for one distinct query", n)
	}

	// A different max_results is a different answer, so it is a different key.
	third := tl.Execute(context.Background(), `{"query":"golang fiber websocket","max_results":2}`)
	if third.IsError {
		t.Fatalf("unexpected error: %s", third.Content)
	}
	if n := ddg.hits.Load(); n != 2 {
		t.Errorf("max_results did not miss the cache (hits=%d)", n)
	}
	if strings.Contains(third.Content, "[3]") {
		t.Errorf("max_results was not honoured:\n%s", third.Content)
	}
}

// ─── arguments ────────────────────────────────────────────────────────────────

func TestArgumentErrorsAreReportedBeforeAnyRequest(t *testing.T) {
	// No URL policy override: a tool that reached the network at all would fail
	// this test by leaving the process, not by what it returned.
	tl := search.New(5)

	if result := tl.Execute(context.Background(), `{"query":""}`); !result.IsError ||
		!strings.Contains(result.Content, "query is required") {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result := tl.Execute(context.Background(), `not-json`); !result.IsError ||
		!strings.Contains(result.Content, "invalid arguments") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDefinitionIsStable(t *testing.T) {
	def := search.New(5).Definition()
	if def.Function.Name != search.ToolName {
		t.Fatalf("tool name = %q", def.Function.Name)
	}
	props, ok := def.Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("no properties in the tool definition")
	}
	for _, want := range []string{"query", "max_results"} {
		if _, ok := props[want]; !ok {
			t.Errorf("the definition lost %q", want)
		}
	}
}

// ─── a wall the markers are the only witness to ───────────────────────────────

const (
	// A Bing wall: no b_algo, no b_no container, and none of the challenge
	// markers either. The only thing that gives it away is a 200 from a search
	// engine that carries no result markup at all. b_notificationContainer is
	// on every Bing page including this one, and "b_no" matched it as a
	// substring, so this page used to be read as Bing's own empty-results page
	// and cached for ten minutes as "No results found."
	bingWallPage = `<!DOCTYPE html><html><head><title>Bing</title></head><body>` +
		`<div id="b_notificationContainer" class="b_notificationContainer"></div>` +
		`<form action="/search"><input name="q" value="anything"></form>` +
		`<div id="b_content"><div class="b_verify">Verify that you are human before continuing.</div></div>` +
		`</body></html>`

	// Bing's real "nothing matched" page: the b_no container, and no results.
	bingNoResultsPage = `<!DOCTYPE html><html><head><title>Bing</title></head><body>` +
		`<div id="b_notificationContainer"></div>` +
		`<ol id="b_results"><li class="b_no"><h1>There are no results for <strong>qqzzxx</strong></h1>` +
		`<ul><li>Check your spelling or try different keywords.</li></ul></li></ol>` +
		`</body></html>`
)

// Both backends walled is a failure, not an empty result set. Telling a model
// "No results found." when the truth is "we were blocked" sends it off to
// answer from memory, and the ten-minute cache made the lie durable.
func TestBothBackendsWalledIsAnErrorNotAnEmptyAnswer(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-anomaly-202.html"), status: http.StatusAccepted}
	bing := &page{body: bingWallPage}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if !result.IsError {
		t.Fatalf("two walls answered as a success: %+v", result)
	}
	if strings.Contains(result.Content, "No results found") {
		t.Errorf("a wall was reported as an empty result set: %q", result.Content)
	}
	for _, want := range []string{"web search backends unavailable right now", "duckduckgo:", "bing:", "fetch_url"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("the refusal does not mention %q: %q", want, result.Content)
		}
	}
	// A wall is the one failure the second user agent can change, so both
	// backends spend their retry.
	if n := bing.hits.Load(); n != 2 {
		t.Errorf("Bing was asked %d times, want 2 (one retry)", n)
	}
}

// The other half of the same check: Bing's own no-results page is an answer.
// Anchoring the marker must not turn "nothing matched" into a wall.
func TestBingNoResultsPageIsAnAnswerNotAWall(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-anomaly-202.html"), status: http.StatusAccepted}
	bing := &page{body: bingNoResultsPage}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"qqzzxx"}`)

	if result.IsError {
		t.Fatalf("a real empty result page was reported as a failure: %s", result.Content)
	}
	if !strings.Contains(result.Content, "No results found.") || !strings.Contains(result.Content, "source: bing") {
		t.Errorf("unexpected content: %q", result.Content)
	}
	if n := bing.hits.Load(); n != 1 {
		t.Errorf("Bing was asked %d times, want 1 — an answer is not retried", n)
	}
}

// Both backends echo the query back into the search box as value="…". A query
// that happens to contain a wall marker therefore prints the marker onto its
// own result page, and the check ran before the parser: searching for
// "challenge-form" walled every backend, every time, for as long as it was
// asked.
func TestAQueryThatSpellsAWallMarkerStillGetsResults(t *testing.T) {
	body := strings.Replace(
		fixture(t, "ddg-html-post.html"),
		`value="golang fiber websocket"`,
		`value="challenge-form anomaly.js"`, 1)
	if !strings.Contains(body, "challenge-form") {
		t.Fatal("the fixture no longer echoes the query, which is the premise of this test")
	}

	ddg := &page{body: body}
	bing := &page{body: fixture(t, "bing-html.html")}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"challenge-form anomaly.js"}`)

	if result.IsError {
		t.Fatalf("a result page was thrown away because the query was in it: %s", result.Content)
	}
	if !strings.Contains(result.Content, "source: duckduckgo") {
		t.Errorf("the answer did not come from the first backend:\n%s", result.Content)
	}
	if n := bing.hits.Load(); n != 0 {
		t.Errorf("Bing was asked %d times for a query DuckDuckGo answered", n)
	}
}

// ─── the cap is a cut, and a cut is not an empty page ─────────────────────────

// oversized is comfortably past maxSearchResponseBytes (512 KB).
func oversizedPadding() string { return strings.Repeat("<!-- pad -->", 60*1024) }

// Everything the reader got to see still counts. A page that overruns the cap
// after its result list has already been read is a complete enough answer.
func TestOversizedPageStillAnswersFromWhatFitInsideTheLimit(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-html-post.html") + oversizedPadding()}
	bing := &page{body: fixture(t, "bing-html.html")}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"golang fiber websocket"}`)

	if result.IsError {
		t.Fatalf("results read before the cut were thrown away: %s", result.Content)
	}
	if !strings.Contains(result.Content, "https://docs.gofiber.io/contrib/websocket/") {
		t.Errorf("unexpected content:\n%s", result.Content)
	}
	if n := bing.hits.Load(); n != 0 {
		t.Errorf("Bing was asked %d times for a query DuckDuckGo answered", n)
	}
}

// A page whose result list is past the cut used to be judged by its markers as
// if the whole thing had been read: no results, no markers, therefore a wall.
// It is neither a wall nor an empty answer — it is an answer that did not fit,
// and saying so is what lets the next backend be tried and the model be told
// something true.
func TestOversizedPageBeforeTheResultsIsTooLargeNotAWall(t *testing.T) {
	pad := oversizedPadding()
	ddg := &page{body: pad + fixture(t, "ddg-html-post.html")}
	bing := &page{body: pad + fixture(t, "bing-html.html")}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"anything"}`)

	if !result.IsError {
		t.Fatalf("a page that was never fully read answered as a success: %+v", result)
	}
	if !strings.Contains(result.Content, "response too large") {
		t.Errorf("the refusal does not say what went wrong: %q", result.Content)
	}
	// Both backends were tried, and neither wasted a retry: the same oversized
	// page comes back the same size under the other user agent.
	if n := ddg.hits.Load(); n != 1 {
		t.Errorf("DuckDuckGo was asked %d times, want 1", n)
	}
	if n := bing.hits.Load(); n != 1 {
		t.Errorf("Bing was asked %d times, want 1", n)
	}
}

// ─── what the retry is for ────────────────────────────────────────────────────

// A 5xx can be transient and the other user agent costs one request, so it is
// worth the second attempt.
func TestServerErrorsAreRetriedOnceThenTheNextBackend(t *testing.T) {
	ddg := &page{body: "upstream is unwell", status: http.StatusInternalServerError}
	bing := &page{body: fixture(t, "bing-html.html")}
	ddgSrv := httptest.NewTLSServer(ddg.handler())
	defer ddgSrv.Close()
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, ddgSrv).
		serve("www.bing.com", bingIP, bingSrv)

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"google translate"}`)

	if result.IsError {
		t.Fatalf("the fallback did not answer: %s", result.Content)
	}
	if !strings.Contains(result.Content, "source: bing") {
		t.Errorf("the answer is not attributed to Bing:\n%s", result.Content)
	}
	if n := ddg.hits.Load(); n != 2 {
		t.Errorf("DuckDuckGo was asked %d times, want 2 (one retry)", n)
	}
	if n := bing.hits.Load(); n != 1 {
		t.Errorf("Bing was asked %d times, want 1", n)
	}
}

// A refused connection answers the same way twice. The retry used to be spent
// on it anyway — a second dial to a host that is not there, plus the second of
// politeness gap in front of it, before the fallback got its turn.
func TestARefusedConnectionIsNotRetried(t *testing.T) {
	gone := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	bing := &page{body: fixture(t, "bing-html.html")}
	bingSrv := httptest.NewTLSServer(bing.handler())
	defer bingSrv.Close()

	h := newHarness().
		serve("html.duckduckgo.com", ddgIP, gone).
		serve("www.bing.com", bingIP, bingSrv)
	gone.Close() // nothing is listening at that address any more

	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))
	result := tl.Execute(context.Background(), `{"query":"google translate"}`)

	if result.IsError {
		t.Fatalf("the fallback did not answer: %s", result.Content)
	}
	if !strings.Contains(result.Content, "source: bing") {
		t.Errorf("the answer is not attributed to Bing:\n%s", result.Content)
	}
	if n := h.dialsTo(ddgIP); n != 1 {
		t.Errorf("the refused backend was dialled %d times, want 1", n)
	}
}

// ─── what is worth remembering ────────────────────────────────────────────────

// An empty answer is as often a wall this code failed to recognise as it is a
// real "nothing matched". Caching it turned one bad minute into ten in which
// the model was told there was nothing to find without a request leaving.
func TestEmptyAnswersAreNotCached(t *testing.T) {
	body := fixture(t, "ddg-html-post.html")
	var hits atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if hits.Add(1) == 1 {
			_, _ = w.Write([]byte(`<html><body><div class="no-results">No results.</div></body></html>`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	h := newHarness().serve("html.duckduckgo.com", ddgIP, srv)
	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))

	first := tl.Execute(context.Background(), `{"query":"golang fiber websocket"}`)
	if first.IsError || !strings.Contains(first.Content, "No results found.") {
		t.Fatalf("unexpected first result: %+v", first)
	}

	second := tl.Execute(context.Background(), `{"query":"golang fiber websocket"}`)
	if second.IsError {
		t.Fatalf("unexpected second result: %s", second.Content)
	}
	if !strings.Contains(second.Content, "https://docs.gofiber.io/contrib/websocket/") {
		t.Errorf("the empty answer was served from the cache:\n%s", second.Content)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("the backend was asked %d times, want 2", n)
	}
}

// ─── arguments, continued ─────────────────────────────────────────────────────

// max_results above the ceiling used to fall back to the configured default,
// which reads as the argument being ignored: a model that asks for 20 wants
// more than five, so it is given the ten it may have.
func TestMaxResultsIsClampedNotDiscarded(t *testing.T) {
	ddg := &page{body: fixture(t, "ddg-html-post.html")}
	srv := httptest.NewTLSServer(ddg.handler())
	defer srv.Close()

	h := newHarness().serve("html.duckduckgo.com", ddgIP, srv)
	tl := search.New(5, search.WithURLPolicy(h.policy()), search.WithPolitenessGap(0))

	over := tl.Execute(context.Background(), `{"query":"golang fiber websocket","max_results":25}`)
	if over.IsError {
		t.Fatalf("unexpected error: %s", over.Content)
	}
	if !strings.Contains(over.Content, "[8]") {
		t.Errorf("max_results:25 was discarded rather than clamped:\n%s", over.Content)
	}
	if strings.Contains(over.Content, "[11]") {
		t.Errorf("max_results:25 was honoured past the ceiling of 10:\n%s", over.Content)
	}

	// 0 and below are still the "unset" spelling and still mean the default.
	for _, arguments := range []string{
		`{"query":"golang fiber websocket"}`,
		`{"query":"golang fiber websocket","max_results":0}`,
		`{"query":"golang fiber websocket","max_results":-3}`,
	} {
		result := tl.Execute(context.Background(), arguments)
		if result.IsError {
			t.Fatalf("%s: %s", arguments, result.Content)
		}
		if !strings.Contains(result.Content, "[5]") || strings.Contains(result.Content, "[6]") {
			t.Errorf("%s did not fall back to the default of 5:\n%s", arguments, result.Content)
		}
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// syncSlice collects values written from an httptest handler goroutine and read
// from the test goroutine.
type syncSlice struct {
	mu     sync.Mutex
	values []string
}

func (s *syncSlice) add(v string) {
	s.mu.Lock()
	s.values = append(s.values, v)
	s.mu.Unlock()
}

func (s *syncSlice) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.values...)
}

// fakeClock stands still until the code under test sleeps.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type syncDurations struct {
	mu     sync.Mutex
	values []time.Duration
}

func (s *syncDurations) add(v time.Duration) {
	s.mu.Lock()
	s.values = append(s.values, v)
	s.mu.Unlock()
}

func (s *syncDurations) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.values...)
}
