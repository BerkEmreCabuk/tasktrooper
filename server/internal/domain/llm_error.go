package domain

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// llmErrorBodyMax bounds how much of a provider's rejection body is kept. The
// body is quoted into run summaries, task comments and logs, and some providers
// echo the whole rejected request back.
const llmErrorBodyMax = 2000

// LLMHTTPError is a provider's non-2xx answer, kept as a type so the agent loop
// can tell the three cases apart instead of guessing from a string. They need
// opposite treatment: a rate limit wants a pause, an over-long request wants a
// smaller request, and a malformed one wants no retry at all — sending the same
// bytes again only spends the wall clock to collect the same rejection.
type LLMHTTPError struct {
	StatusCode int
	Body       string
	// Provider and Model name the account the refusal belongs to. They are
	// stamped on by the caller that knows them (the multi-provider client, which
	// resolved both before dialling) because the HTTP client itself only knows a
	// base URL. Empty when unknown — the user-facing text omits what it does not
	// know rather than inventing a name.
	Provider string
	Model    string
	// RetryAfter is the wait the provider itself asked for, when it sent a
	// Retry-After header. Zero means it said nothing, and the caller falls back
	// to its own curve. Honouring the provider's number is strictly better than
	// any curve we invent: it is the only figure that knows when the window
	// actually reopens.
	RetryAfter time.Duration
}

// NewLLMHTTPError builds the error from a raw response body, trimming it to
// something a comment or log line can carry.
func NewLLMHTTPError(statusCode int, body []byte) *LLMHTTPError {
	return &LLMHTTPError{StatusCode: statusCode, Body: TruncateHead(string(body), llmErrorBodyMax)}
}

// Error keeps the wording the adapters used before this type existed, so logs
// and stored run summaries read the same across the change.
func (e *LLMHTTPError) Error() string {
	return fmt.Sprintf("llm returned %d: %s", e.StatusCode, e.Body)
}

// Retryable reports whether sending the identical request again could succeed.
func (e *LLMHTTPError) Retryable() bool {
	switch e.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return true
	}
	return e.StatusCode >= http.StatusInternalServerError
}

// quotaExhaustedMarkers are the ways providers say "this account has nothing
// left to spend", as opposed to "you are going too fast". Only the first kind
// makes retrying pointless, so only the first kind may be told to the user as
// such — a provider that says merely "Rate limit exceeded" (the case that sent
// us here: an OpenAI-compatible endpoint answering `{"type":"rate_limited",
// "code":"1300"}` with no headers at all) is deliberately NOT matched, and gets
// the one honest sentence that covers both.
var quotaExhaustedMarkers = []string{
	"insufficient_quota",
	"insufficient quota",
	"exceeded your current quota",
	"quota exceeded",
	"quota_exceeded",
	"out of credit",
	"credit balance",
	"insufficient balance",
	"insufficient_funds",
	"billing",
	"payment required",
	"plan limit",
	"monthly limit",
}

// statusOverloaded is Anthropic's 529 "overloaded_error" — net/http has no
// constant for it because it is not a standard status.
const statusOverloaded = 529

// RateLimited reports whether the provider refused this call for pacing or
// quota reasons rather than for anything about the request itself.
func (e *LLMHTTPError) RateLimited() bool {
	if e.StatusCode == http.StatusTooManyRequests || e.StatusCode == http.StatusPaymentRequired || e.StatusCode == statusOverloaded {
		return true
	}
	if e.StatusCode != http.StatusServiceUnavailable && e.StatusCode != http.StatusForbidden {
		return false
	}
	body := strings.ToLower(e.Body)
	for _, marker := range []string{"rate limit", "rate_limit", "rate_limited", "resource_exhausted", "too many requests", "quota", "overloaded_error", "overloaded"} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// QuotaExhausted reports whether the refusal was the account running out rather
// than running fast. False also covers "the body did not say", which is the
// common case; callers must treat it as "not known to be exhausted", never as
// "known to be temporary".
func (e *LLMHTTPError) QuotaExhausted() bool {
	// 529 means the API is overloaded, not that this account has spent
	// anything — it must never be reported as exhausted.
	if !e.RateLimited() || e.StatusCode == statusOverloaded {
		return false
	}
	if e.StatusCode == http.StatusPaymentRequired {
		return true
	}
	body := strings.ToLower(e.Body)
	for _, marker := range quotaExhaustedMarkers {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// RateLimitNoticePrefix marks a transcript line as a rate-limit notice rather
// than a run failure. Clients key their warning styling off it, the same way
// they key the error bubble off "**Error:**".
const RateLimitNoticePrefix = "**Rate limit:**"

// LLMRateLimit is the typed, user-safe summary of a provider rate limit. It
// exists so the fact survives every `fmt.Errorf("...: %w", err)` on the way up
// to whatever writes text on a user's screen, without that layer having to
// parse a string or, worse, paste the provider's JSON into the chat.
type LLMRateLimit struct {
	Provider string
	Model    string
	// Exhausted is set only when the provider said so. When it is false the
	// message must not claim the limit is temporary either — see UserMessage.
	Exhausted  bool
	RetryAfter time.Duration
}

// RateLimitOf reports the rate limit behind err, anywhere in its wrap chain.
func RateLimitOf(err error) (LLMRateLimit, bool) {
	var he *LLMHTTPError
	if !errors.As(err, &he) || !he.RateLimited() {
		return LLMRateLimit{}, false
	}
	return LLMRateLimit{
		Provider:   he.Provider,
		Model:      he.Model,
		Exhausted:  he.QuotaExhausted(),
		RetryAfter: he.RetryAfter,
	}, true
}

// UserMessage is the sentence a person sees. It never contains the provider's
// body, its status code, or the run's attempt trace: those are in the logs,
// which is where someone debugging this looks, and they are noise or alarm to
// everyone else.
func (r LLMRateLimit) UserMessage() string {
	var b strings.Builder
	if r.Exhausted {
		b.WriteString("The model provider says this account's quota is used up")
		b.WriteString(r.attribution())
		b.WriteString(". Retrying will not help until the quota is renewed — check the provider account's usage or billing, or switch to another model.")
		return b.String()
	}
	// Not distinguishable: the provider refused for limit reasons but did not say
	// whether the window reopens in a minute or the plan is spent. One sentence
	// that is true either way beats a guess dressed as a diagnosis.
	b.WriteString("The model provider is rate-limiting this account, or its quota is used up")
	b.WriteString(r.attribution())
	b.WriteString(". Wait a moment and try again; if it keeps happening, check the provider account's quota or switch to another model.")
	return b.String()
}

// attribution names the account when it is known, and says nothing when it is
// not.
func (r LLMRateLimit) attribution() string {
	switch {
	case r.Provider != "" && r.Model != "":
		return fmt.Sprintf(" (provider: %s, model: %s)", r.Provider, r.Model)
	case r.Provider != "":
		return fmt.Sprintf(" (provider: %s)", r.Provider)
	case r.Model != "":
		return fmt.Sprintf(" (model: %s)", r.Model)
	}
	return ""
}

// ContextOverflow reports whether the request was rejected for being too long.
//
// Providers disagree on how to say it — OpenAI-compatible servers return 400
// with "context length", Anthropic returns "prompt is too long", others use 413
// — so the status alone cannot decide it. Getting this right matters more than
// the other classes: an over-long conversation is the one failure that a plain
// retry can never fix, because the second attempt sends exactly what was just
// refused.
func (e *LLMHTTPError) ContextOverflow() bool {
	if e.StatusCode == http.StatusRequestEntityTooLarge {
		return true
	}
	if e.StatusCode != http.StatusBadRequest && e.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	body := strings.ToLower(e.Body)
	for _, marker := range []string{
		"context length",
		"context_length",
		"maximum context",
		"context window",
		"prompt is too long",
		"input is too long",
		"too many tokens",
		"token limit",
		"reduce the length",
		"exceeds the maximum",
	} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}
