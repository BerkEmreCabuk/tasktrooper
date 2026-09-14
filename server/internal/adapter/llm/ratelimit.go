package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// RateLimitError is what a provider returns when it refuses a call for pacing
// reasons (429, or 503 while shedding load). It carries the wait the provider
// asked for so the caller can obey it instead of guessing.
type RateLimitError struct {
	Endpoint   string
	StatusCode int
	RetryAfter time.Duration
	Body       string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.Endpoint, e.StatusCode, e.Body)
}

// newRateLimitError builds a RateLimitError for a rate-limited HTTP response,
// or returns nil when the status is a plain failure that must not be retried.
func newRateLimitError(endpoint string, resp *http.Response, body string) *RateLimitError {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return nil
	}
	return &RateLimitError{
		Endpoint:   endpoint,
		StatusCode: resp.StatusCode,
		RetryAfter: retryHint(resp.Header),
		Body:       body,
	}
}

// resetHeaders are the headers an OpenAI-compatible provider uses to say when
// the window it just refused us on reopens. They are read in addition to
// Retry-After, not instead of it: many providers send only these.
var resetHeaders = []string{
	"X-RateLimit-Reset-Requests",
	"X-RateLimit-Reset-Tokens",
	"X-RateLimit-Reset",
}

// retryHint reports the wait the provider itself asked for, reading every form
// it might have used, and zero when it said nothing.
//
// Retry-After wins when present — it is the one header that means only this.
// Otherwise the reset countdowns are used, and of those the longest, because a
// 429 is a refusal by whichever limit is still shut and the shorter window
// reopening does not make the call allowed. Waiting slightly too long costs one
// slow reply; waiting too little costs the whole run, which is the failure that
// sends people here.
//
// Reading only Retry-After was not enough: a provider that states its limit
// solely as a reset countdown left us with no number at all, so chat fell back
// to a curve we invented and spent all three of its attempts inside a window
// whose length the provider had already told us.
func retryHint(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	if d := parseRetryAfter(h.Get("Retry-After")); d > 0 {
		return d
	}
	if ms, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After-Ms"))); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	var longest time.Duration
	for _, name := range resetHeaders {
		if d := parseResetHint(h.Get(name)); d > longest {
			longest = d
		}
	}
	return longest
}

// parseResetHint reads a reset countdown, which providers write either as a Go
// duration ("1.5s", "2m59.56s", "88ms") or as bare seconds.
func parseResetHint(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	if secs, err := strconv.ParseFloat(value, 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	return 0
}

// parseRetryAfter reads both Retry-After forms: delay seconds and HTTP date.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// isRateLimited also matches providers that surface the limit only as text —
// the Vertex SDK returns RESOURCE_EXHAUSTED, and OpenAI-compatible proxies
// wrap the status in the message body.
func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "rate_limited") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "too many requests")
}

// retryAfterOf reports the wait a provider asked for, when it asked for one.
// Both error shapes carry it: embeddings fail as *RateLimitError, chat as
// *domain.LLMHTTPError, and the header means the same thing on either path.
func retryAfterOf(err error) time.Duration {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return rle.RetryAfter
	}
	var he *domain.LLMHTTPError
	if errors.As(err, &he) {
		return he.RetryAfter
	}
	return 0
}

// httpError builds the typed chat-path error, keeping the provider's own
// Retry-After instead of dropping it at the adapter boundary.
func httpError(resp *http.Response, body []byte) *domain.LLMHTTPError {
	e := domain.NewLLMHTTPError(resp.StatusCode, body)
	e.RetryAfter = retryHint(resp.Header)
	if e.RateLimited() {
		logRateLimitHeaders(resp)
	}
	return e
}

// secretHeaderMarkers are the substrings that make a header unloggable whatever
// else its name looks like. The exclusion is checked first and wins outright:
// a header called `x-api-key-remaining` must not be printed because it matched
// "remaining".
//
// Bare "token" and "key" are deliberately not markers: providers state their
// token budgets as `x-ratelimit-remaining-tokens`, which is precisely the
// number this capture exists to find. The credential shapes are named exactly
// enough to catch a secret without swallowing a budget.
var secretHeaderMarkers = []string{"authorization", "cookie", "auth", "api-key", "apikey", "access-token", "refresh-token", "id-token", "secret", "credential", "signature", "bearer"}

// rateLimitMarkers are the shapes providers use to state a limit. The list is
// deliberately loose: the point of capturing these is that we do NOT know what
// this provider sends, and a name we failed to anticipate is exactly the one
// worth seeing.
var rateLimitMarkers = []string{"retry-after", "ratelimit", "rate-limit", "reset", "remaining", "limit", "quota", "usage"}

// rateLimitHeaders picks the rate-limit-relevant headers out of a response,
// leaving out anything key-like.
//
// It exists because the provider behind the incident sends no Retry-After and
// no reset header, and its own dashboard's quota indicator is broken — so there
// is no way to tell "bursting over a per-minute window" from "plan spent"
// except by writing down, once per refusal, exactly what it does send. An empty
// map is a real answer too: it proves the hedge in the user-facing message is
// warranted rather than assumed.
func rateLimitHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	if h == nil {
		return out
	}
	for name, values := range h {
		lower := strings.ToLower(name)
		if containsAny(lower, secretHeaderMarkers) {
			continue
		}
		if !containsAny(lower, rateLimitMarkers) {
			continue
		}
		out[lower] = strings.Join(values, ",")
	}
	return out
}

func containsAny(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// logRateLimitHeaders writes the one structured line per refusal. It is cheap
// enough (a handful of string ops on a path that only runs when a call was
// already refused) to leave on permanently.
func logRateLimitHeaders(resp *http.Response) {
	headers := rateLimitHeaders(resp.Header)
	log.Warn().
		Int("status", resp.StatusCode).
		Int("header_count", len(headers)).
		Interface("rate_limit_headers", headers).
		Msg("provider rate-limit response headers")
}

const (
	defaultEmbedRetryBackoff = 2 * time.Second
	defaultEmbedMaxRetryWait = 60 * time.Second
)

// backgroundGrace is how long a background call hangs back after the window
// opens before claiming the slot. Without it, a reindex that has been waiting
// takes the very instant the slot frees, and a chat request arriving in the
// same moment queues behind it; with it, the interactive caller wins that race.
// It is small enough to be invisible to indexing throughput.
const backgroundGrace = 50 * time.Millisecond

// accountLimiter paces every call made against one provider account and retries
// the rate-limited ones.
//
// It used to pace embeddings only, which was the bug behind chat getting a 429
// on the first attempt with nobody else on the system: chat spent the same
// account's window, so the counter protecting that window was measuring a
// fraction of the traffic hitting it. One account is one limiter — the quota
// belongs to the provider, not to the endpoint or the call site.
type accountLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	// next is the earliest wall-clock time the next call may start. Both the
	// steady-state spacing and a provider's Retry-After push it forward.
	next       time.Time
	maxRetries int
	backoff    time.Duration
	maxWait    time.Duration
}

func (l *accountLimiter) configure(cfg domain.EmbeddingConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minInterval = 0
	if cfg.RequestsPerMinute > 0 {
		l.minInterval = time.Minute / time.Duration(cfg.RequestsPerMinute)
	}
	l.maxRetries = cfg.MaxRetries
	if l.maxRetries < 0 {
		l.maxRetries = 0
	}
	l.backoff = cfg.RetryBackoff
	if l.backoff <= 0 {
		l.backoff = defaultEmbedRetryBackoff
	}
	l.maxWait = cfg.MaxRetryWait
	if l.maxWait <= 0 {
		l.maxWait = defaultEmbedMaxRetryWait
	}
}

func (l *accountLimiter) settings() (maxRetries int, backoff, maxWait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	backoff = l.backoff
	if backoff <= 0 {
		backoff = defaultEmbedRetryBackoff
	}
	maxWait = l.maxWait
	if maxWait <= 0 {
		maxWait = defaultEmbedMaxRetryWait
	}
	return l.maxRetries, backoff, maxWait
}

// reserve claims the next slot and returns how long the caller must wait for it.
func (l *accountLimiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	wait := time.Duration(0)
	if l.next.After(now) {
		wait = l.next.Sub(now)
	}
	start := now.Add(wait)
	l.next = start.Add(l.minInterval)
	return wait
}

// penalize holds every following call back by d, so one 429 slows the whole
// stream instead of only the call that hit it.
func (l *accountLimiter) penalize(d time.Duration) {
	if d <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(l.next) {
		l.next = until
	}
}

// tryReserve claims the slot only if it is free right now, and otherwise
// reports how long is left on the window without claiming anything. Not
// claiming is the point: a background caller that held a reservation while it
// slept would put every chat request behind it.
func (l *accountLimiter) tryReserve() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.next.After(now) {
		return l.next.Sub(now), false
	}
	l.next = now.Add(l.minInterval)
	return 0, true
}

// acquire waits for this call's turn in the account's window.
//
// Interactive callers take a firm reservation and are served in arrival order.
// Background callers (marked on the context) never hold one: they wait, look
// again, and take the slot only when it is free, so any interactive call that
// shows up in the meantime goes first. Background work therefore yields to a
// person waiting on a reply — it does not stop, it just never goes ahead.
func (l *accountLimiter) acquire(ctx context.Context) error {
	if domain.IsBackgroundLLM(ctx) {
		return l.acquireBackground(ctx)
	}
	wait := l.reserve()
	if wait <= 0 {
		return ctx.Err()
	}
	return sleepCtx(ctx, wait)
}

func (l *accountLimiter) acquireBackground(ctx context.Context) error {
	for {
		wait, ok := l.tryReserve()
		if ok {
			return ctx.Err()
		}
		if err := sleepCtx(ctx, wait+backgroundGrace); err != nil {
			return err
		}
	}
}

// guard runs one non-embedding call (chat, streamed or not) under the same
// account pacing, and makes its rate limit slow everything else down too. It
// does not retry: chat retries are the agent loop's decision, since only the
// loop can shrink the conversation instead of re-sending it.
// The provider and model are passed in because nothing below this point knows
// them: the limiter is keyed by account and the HTTP client only has a base
// URL. Without them the rate-limit warning named no account at all, so an
// incident could not be tied to a provider without reading the code — and the
// user-facing message had nothing to attribute the limit to.
func (l *accountLimiter) guard(ctx context.Context, provider domain.LLMProviderType, model string, fn func(context.Context) error) error {
	if err := l.acquire(ctx); err != nil {
		return err
	}
	err := fn(ctx)
	// Stamp the account onto the typed error while it is still identifiable, so
	// the identity survives every wrapping between here and the user's screen.
	var he *domain.LLMHTTPError
	if errors.As(err, &he) {
		if he.Provider == "" {
			he.Provider = string(provider)
		}
		if he.Model == "" {
			he.Model = model
		}
	}
	if isRateLimited(err) {
		_, backoff, maxWait := l.settings()
		wait := retryAfterOf(err)
		hinted := wait > 0
		if wait <= 0 {
			wait = backoff
		}
		if wait > maxWait {
			wait = maxWait
		}
		l.penalize(wait)
		// Whether the provider told us when its window reopens is the first
		// thing anyone needs when a chat run dies on a 429, and it is invisible
		// in the error text.
		log.Warn().Err(err).
			Str("provider", string(provider)).
			Str("model", model).
			Dur("wait", wait).
			Bool("provider_hinted", hinted).
			Msg("chat rate limited, holding the account back")
	}
	return err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// do runs fn under the pacing rules, retrying while the provider rate-limits us.
func (l *accountLimiter) do(ctx context.Context, fn func(context.Context) ([]float32, error)) ([]float32, error) {
	maxRetries, backoff, maxWait := l.settings()
	for attempt := 0; ; attempt++ {
		if err := l.acquire(ctx); err != nil {
			return nil, err
		}
		vec, err := fn(ctx)
		if err == nil {
			return vec, nil
		}
		if !isRateLimited(err) {
			return nil, err
		}

		wait := retryAfterOf(err)
		if wait <= 0 {
			wait = backoff << attempt
			if wait <= 0 { // shift overflow on a long streak
				wait = maxWait
			}
		}
		if wait > maxWait {
			wait = maxWait
		}
		// Delay the whole stream, not just this retry: the next chunk would
		// otherwise walk straight into the same limit.
		l.penalize(wait)

		if attempt >= maxRetries {
			return nil, err
		}
		log.Warn().
			Err(err).
			Int("attempt", attempt+1).
			Int("max_retries", maxRetries).
			Dur("wait", wait).
			Msg("embedding rate limited, retrying")
	}
}
