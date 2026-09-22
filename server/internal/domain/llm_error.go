package domain

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// llmErrorBodyMax bounds how much of a provider's rejection body is kept — it
// is quoted into summaries and logs, and some providers echo the whole request.
const llmErrorBodyMax = 2000

// LLMHTTPError is a provider's non-2xx answer, kept as a type so the agent loop
// can tell the three cases apart instead of guessing from a string: a rate
// limit wants a pause, an over-long request wants a smaller request, and a
// malformed one wants no retry at all.
type LLMHTTPError struct {
	StatusCode int
	Body       string
	// Provider and Model name the account the refusal belongs to, stamped by
	// the caller that resolved them; empty when unknown, and the user-facing
	// text omits what it does not know.
	Provider string
	Model    string
	// RetryAfter is the wait the provider itself asked for via Retry-After;
	// zero means it said nothing and the caller falls back to its own curve —
	// the provider's figure is the only one that knows when the window
	// reopens.
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
// left to spend", as opposed to "too fast" — only the first kind makes retrying
// pointless. A provider that merely says "Rate limit exceeded" (e.g. an
// OpenAI-compatible endpoint answering `{"type":"rate_limited","code":"1300"}`
// with no headers) is deliberately NOT matched.
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

// statusOverloaded is Anthropic's 529 "overloaded_error", not a standard status.
const statusOverloaded = 529

// RateLimited reports whether the provider refused for pacing or quota reasons
// rather than for anything about the request itself.
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
// than running fast; false also covers "the body did not say", so callers must
// read it as "not known to be exhausted", never as "known to be temporary".
func (e *LLMHTTPError) QuotaExhausted() bool {
	// 529 means the API is overloaded, not that this account spent anything.
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
// than a run failure; clients key their warning styling off it.
const RateLimitNoticePrefix = "**Rate limit:**"

// LLMRateLimit is the typed, user-safe summary of a provider rate limit, so the
// fact survives every wrap on the way up to whatever writes text on screen
// without parsing a string or pasting the provider's JSON into the chat.
type LLMRateLimit struct {
	Provider string
	Model    string
	// Exhausted is set only when the provider said so; when false the message
	// must not claim the limit is temporary either.
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

// UserMessage is what a person sees; it never contains the provider's body,
// status code, or attempt trace — those belong in the logs.
func (r LLMRateLimit) UserMessage() string {
	var b strings.Builder
	if r.Exhausted {
		b.WriteString("The model provider says this account's quota is used up")
		b.WriteString(r.attribution())
		b.WriteString(". Retrying will not help until the quota is renewed — check the provider account's usage or billing, or switch to another model.")
		return b.String()
	}
	// Not distinguishable: one sentence true whether the window reopens in a
	// minute or the plan is spent beats a guess dressed as a diagnosis.
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
// Providers disagree on the wording (OpenAI-compatible: 400 "context length";
// Anthropic: "prompt is too long"; others 413), so status alone cannot decide
// it. Getting this right matters: a plain retry sends exactly what was just
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
