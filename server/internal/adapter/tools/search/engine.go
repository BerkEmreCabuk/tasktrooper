package search

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/rs/zerolog/log"
)

// The two backends, in the order they are tried.
//
// DuckDuckGo's HTML endpoint is first because it answers a plain form POST with
// clean markup and no tracking redirect. It also puts up a bot wall on a GET —
// every GET probe from this machine came back 202 with an "anomaly" challenge
// page and zero results — so the POST form, the browser headers and the
// wall detection below are all load-bearing, not decoration. Bing is the
// fallback: noisier markup, results wrapped in a /ck/a redirect, but it answers
// a GET.
const (
	ddgEndpoint  = "https://html.duckduckgo.com/html/"
	bingEndpoint = "https://www.bing.com/search"

	sourceDuckDuckGo = "duckduckgo"
	sourceBing       = "bing"
)

const (
	// minRequestGap is per-process politeness. Two searches back to back from
	// one IP is the cheapest way to earn the bot wall this code exists to
	// survive, and a second of latency is nothing against a tool call that
	// already costs a network round trip.
	minRequestGap = 1 * time.Second

	cacheEntries = 64
	cacheTTL     = 10 * time.Minute
)

// browserUserAgents is rotated on the retry. A backend that refused the first
// answer sometimes accepts the second from a different client string; sending
// the same one twice is just the same request twice.
var browserUserAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
}

// errBotWall is the one failure that is worth changing backends over: the
// request succeeded at the transport level and the answer was a challenge page.
var errBotWall = errors.New("bot challenge page")

// errTruncated is a page that did not fit in maxSearchResponseBytes and whose
// result list was still ahead of the cut. It is deliberately not a wall: the
// backend answered, the answer is just too big to read, and calling it a
// challenge would blame the wrong thing and spend a pointless retry. The next
// backend gets the query and the model is told what actually happened.
var errTruncated = errors.New("response too large")

// httpStatusError carries the code next to the message so the retry decision
// can look at the number instead of re-reading the sentence.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

type result struct {
	Title   string
	URL     string
	Snippet string
}

// backend is one search engine: how to ask it, how to read the answer, and the
// two markers that tell an empty result page apart from a wall.
type backend struct {
	name string
	// request builds the outbound call. It sets nothing but the method, the URL
	// and the body; the browser headers are applied by attempt.
	request func(ctx context.Context, query string) (*http.Request, error)
	parse   func(page string) []result
	// resultMarker is the class every page that actually rendered results
	// carries. Its absence on a 200 that parsed to nothing means the page was
	// not a result page at all.
	resultMarker string
	// emptyMarker is the class on the backend's own "nothing matched" page,
	// which is a real answer and must not send the query to the next backend.
	//
	// Both are matched as whole class tokens, never as substrings. "b_no" is a
	// prefix of Bing's b_notificationContainer, which is on every Bing page
	// including a wall — as a substring it made the empty-page test true for
	// every Bing response, so no Bing wall was ever detected and each one was
	// cached for ten minutes as "No results found."
	emptyMarker string
}

var backends = []backend{
	{
		name:         sourceDuckDuckGo,
		request:      ddgRequest,
		parse:        parseDuckDuckGo,
		resultMarker: "result__a",
		emptyMarker:  "no-results",
	},
	{
		name:         sourceBing,
		request:      bingRequest,
		parse:        parseBing,
		resultMarker: "b_algo",
		emptyMarker:  "b_no",
	},
}

// search answers a query, or explains that neither backend would.
func (s *searchTool) search(ctx context.Context, query string, maxResults int) (string, string, error) {
	key := cacheKey(query, maxResults)
	if content, source, ok := s.cache.get(key, s.now()); ok {
		return content, source, nil
	}

	reasons := make([]string, 0, len(backends))
	for _, b := range backends {
		results, err := s.runBackend(ctx, b, query)
		if err != nil {
			reasons = append(reasons, b.name+": "+modelSafeReason(b.name, err))
			continue
		}
		content := format(results, maxResults, b.name)
		// An empty answer is not worth ten minutes of memory. It is as often a
		// wall this code failed to recognise as it is a real "nothing matched",
		// and caching it turns one bad minute into ten in which the model is
		// told there is nothing to find without a single request leaving.
		if len(results) > 0 {
			s.cache.put(key, content, b.name, s.now())
		}
		return content, b.name, nil
	}

	return "", "", fmt.Errorf(
		"web search backends unavailable right now (%s) — try again later or fetch a known URL with fetch_url",
		strings.Join(reasons, "; "))
}

// modelSafeReason is the part of a backend failure the model is allowed to
// read. A urlguard rejection names the host and the address behind it; that is
// a resolved-IP disclosure — the pod's view of the network, handed to whoever can
// steer a query — so it collapses to one fixed sentence and the detail goes to
// the log, where an operator can still tell the two apart.
func modelSafeReason(name string, err error) string {
	if errors.Is(err, urlguard.ErrBlocked) {
		log.Warn().Str("backend", name).Err(err).Msg("search backend destination refused by the outbound URL policy")
		return "blocked by the outbound URL policy"
	}
	return oneLine(err.Error())
}

// runBackend spends one backend's whole budget: a first attempt and a single
// retry under the other user agent. Anything past that is two backends' worth of
// latency for a query the model can ask again.
//
// The retry is only spent on failures a different client string or another
// second can plausibly change. A blocked destination, a name that does not
// resolve, a refused connection, a body too large and a cancelled context all
// answer identically the second time: the retry buys nothing and costs a real
// second of politeness gap plus one more outbound request to be rate limited
// for.
func (s *searchTool) runBackend(ctx context.Context, b backend, query string) ([]result, error) {
	var last error
	for _, ua := range browserUserAgents {
		results, err := s.attempt(ctx, b, query, ua)
		if err == nil {
			return results, nil
		}
		last = err
		if ctx.Err() != nil || !worthRetrying(err) {
			break
		}
	}
	return nil, last
}

// worthRetrying reports whether a second attempt under the other user agent has
// any chance of a different answer: a wall, a 429, or a server-side 5xx.
func worthRetrying(err error) bool {
	if errors.Is(err, errBotWall) {
		return true
	}
	var status *httpStatusError
	if errors.As(err, &status) {
		return status.code == http.StatusTooManyRequests || status.code >= 500
	}
	return false
}

func (s *searchTool) attempt(ctx context.Context, b backend, query, userAgent string) ([]result, error) {
	req, err := b.request(ctx, query)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	if err := s.limiter.wait(ctx); err != nil {
		return nil, err
	}

	resp, err := s.do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// One byte past the cap, so a page that ends exactly at the limit is still
	// a whole page and anything longer is *known* to be a fragment. Reading
	// exactly the cap and then judging what came back by its markers cannot
	// tell "this backend has nothing" from "the answer was cut off before its
	// result list" — and the second was being reported as the first.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchResponseBytes+1))
	if err != nil {
		return nil, err
	}
	truncated := len(body) > maxSearchResponseBytes
	if truncated {
		body = body[:maxSearchResponseBytes]
	}
	page := string(body)

	// 202 is what DuckDuckGo answers a request it does not believe: the status
	// says accepted, the body is a challenge, and nothing about it is a result.
	if resp.StatusCode == http.StatusAccepted {
		return nil, errBotWall
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &httpStatusError{code: resp.StatusCode}
	}

	// A page that parsed is an answer, whatever else is written on it. The wall
	// markers are only consulted once the parser has come up empty, because
	// both backends echo the query back into a value="…" attribute on the search
	// box: a query for "anomaly.js" or "challenge-form" printed its own marker
	// onto a perfectly good result page and walled every attempt of every
	// backend for that query, every time it was asked.
	if results := b.parse(page); len(results) > 0 {
		return results, nil
	}
	// What was read is all there was room for, and the result list may simply
	// be past the cut. Not a wall — say so, and let the next backend answer.
	if truncated {
		return nil, errTruncated
	}
	if isBotWall(page) {
		return nil, errBotWall
	}
	if !pageHasClass(page, b.resultMarker) && !pageHasClass(page, b.emptyMarker) {
		// A 200 carrying neither results, result markup, nor the backend's own
		// empty-result page is a wall wearing a success code.
		return nil, errBotWall
	}
	return nil, nil
}

// do issues a request whose destination is a constant in this file — but the
// name still goes through the guard, because a poisoned answer for
// html.duckduckgo.com is the whole attack and this pod's DNS is not something
// this code gets to trust.
//
// The redirect chain is pinned to the endpoint's own host. There is no
// credential to leak any more, but a search backend that can redirect this
// process to an arbitrary host is a request forgery primitive on its own, and
// the ordinary http->https or path-move redirect still works.
func (s *searchTool) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	target, err := s.policy.Validate(ctx, req.URL.String())
	if err != nil {
		return nil, err
	}
	client := s.policy.ClientFor(target, searchTimeout)
	client.CheckRedirect = s.policy.SameHostRedirect(target.URL)
	return client.Do(req)
}

// ddgRequest is the POST form that works. The GET form of the same endpoint —
// and lite.duckduckgo.com, and the vqd + links.duckduckgo.com/d.js path — all
// answer 202 with a challenge page from this network. The body is spelled out
// rather than built with url.Values because the field order below is the one
// that was verified against the live endpoint.
func ddgRequest(ctx context.Context, query string) (*http.Request, error) {
	body := "q=" + url.QueryEscape(query) + "&b=&kl=wt-wt"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ddgEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func bingRequest(ctx context.Context, query string) (*http.Request, error) {
	endpoint := bingEndpoint + "?q=" + url.QueryEscape(query) + "&setlang=en"
	return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
}

// format renders what the tool has always rendered — a numbered block per
// result — plus the backend the answer came from, so a reader can tell a
// DuckDuckGo answer from the fallback.
func format(results []result, maxResults int, source string) string {
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if len(parts) >= maxResults {
			break
		}
		block := fmt.Sprintf("[%d] %s\nURL: %s", len(parts)+1, r.Title, r.URL)
		if r.Snippet != "" {
			block += "\n" + r.Snippet
		}
		parts = append(parts, block)
	}
	if len(parts) == 0 {
		return "No results found.\n\nsource: " + source
	}
	return strings.Join(parts, "\n\n") + "\n\nsource: " + source
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ─── politeness ───────────────────────────────────────────────────────────────

// limiter spaces outbound requests. A caller takes the next slot under the lock
// and then waits for it with the lock released.
//
// Holding the mutex across the sleep — the previous shape — queued concurrent
// callers on Lock, where no context deadline can reach them: a cancelled
// request kept its goroutine parked for the rest of the gap, and the caller
// behind it waited out a deadline it had already blown. Reserving rather than
// measuring also fixes the burst, where N callers arriving together all read
// the same last and all decided enough time had passed.
type limiter struct {
	mu  sync.Mutex
	gap time.Duration
	// next is the earliest a following request may leave. It is advanced when
	// the slot is handed out rather than after the sleep, which is what makes
	// two concurrent callers take two different slots.
	next  time.Time
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func newLimiter(gap time.Duration) *limiter {
	return &limiter{gap: gap, now: time.Now, sleep: sleepCtx}
}

func (l *limiter) wait(ctx context.Context) error {
	now := l.now()

	l.mu.Lock()
	slot := now
	if l.gap > 0 {
		if l.next.After(now) {
			slot = l.next
		}
		l.next = slot.Add(l.gap)
	}
	l.mu.Unlock()

	// A caller that gives up here keeps its reservation rather than handing it
	// back. That is the cautious direction: releasing it would let a cancelled
	// request quietly buy the next one an early exit.
	if d := slot.Sub(now); d > 0 {
		return l.sleep(ctx, d)
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ─── cache ────────────────────────────────────────────────────────────────────

// cache keeps a handful of recent answers. An agent loop that asks the same
// question twice in a minute is common — a re-read after a failed fetch, a
// retry after a tool error — and each repeat is another chance to be walled.
type cache struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	entries map[string]cacheEntry
	order   []string // least recently used first
}

type cacheEntry struct {
	content string
	source  string
	expires time.Time
}

func newCache(max int, ttl time.Duration) *cache {
	return &cache{max: max, ttl: ttl, entries: map[string]cacheEntry{}}
}

func cacheKey(query string, maxResults int) string {
	return fmt.Sprintf("%d\x00%s", maxResults, strings.ToLower(strings.Join(strings.Fields(query), " ")))
}

func (c *cache) get(key string, now time.Time) (string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return "", "", false
	}
	if now.After(e.expires) {
		delete(c.entries, key)
		c.forget(key)
		return "", "", false
	}
	c.touch(key)
	return e.content, e.source, true
}

func (c *cache) put(key, content, source string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{content: content, source: source, expires: now.Add(c.ttl)}
	c.touch(key)
	for len(c.order) > c.max {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

func (c *cache) touch(key string) {
	c.forget(key)
	c.order = append(c.order, key)
}

func (c *cache) forget(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
