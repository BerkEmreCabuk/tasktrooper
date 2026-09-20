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

type RateLimitError struct {
	Endpoint   string
	StatusCode int
	RetryAfter time.Duration
	Body       string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.Endpoint, e.StatusCode, e.Body)
}

const statusOverloaded = 529

func newRateLimitError(endpoint string, resp *http.Response, body string) *RateLimitError {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != statusOverloaded {
		return nil
	}
	return &RateLimitError{
		Endpoint:   endpoint,
		StatusCode: resp.StatusCode,
		RetryAfter: retryHint(resp.Header),
		Body:       body,
	}
}

var resetHeaders = []string{
	"X-RateLimit-Reset-Requests",
	"X-RateLimit-Reset-Tokens",
	"X-RateLimit-Reset",
}

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
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "529") ||
		strings.Contains(msg, "overloaded")
}

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

func httpError(resp *http.Response, body []byte) *domain.LLMHTTPError {
	e := domain.NewLLMHTTPError(resp.StatusCode, body)
	e.RetryAfter = retryHint(resp.Header)
	if e.RateLimited() {
		logRateLimitHeaders(resp)
	}
	return e
}

var secretHeaderMarkers = []string{"authorization", "cookie", "auth", "api-key", "apikey", "access-token", "refresh-token", "id-token", "secret", "credential", "signature", "bearer"}

var rateLimitMarkers = []string{"retry-after", "ratelimit", "rate-limit", "reset", "remaining", "limit", "quota", "usage"}

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

const backgroundGrace = 50 * time.Millisecond

type accountLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
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

func (l *accountLimiter) guard(ctx context.Context, provider domain.LLMProviderType, model string, fn func(context.Context) error) error {
	if err := l.acquire(ctx); err != nil {
		return err
	}
	err := fn(ctx)

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
