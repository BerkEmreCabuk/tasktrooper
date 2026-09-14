package llmprovider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ProviderErrorKind names why an upstream provider refused a call, so the UI can
// say "your quota ran out" instead of leaving the user to guess from a status
// code. The zero value (ProviderErrorUnknown) means "no better answer than the
// provider's own message".
type ProviderErrorKind string

const (
	ProviderErrorUnknown ProviderErrorKind = ""
	// ProviderErrorQuotaExhausted is a spent allowance: the key is valid, there
	// is simply nothing left on it until the plan renews or is topped up.
	ProviderErrorQuotaExhausted ProviderErrorKind = "provider_quota_exhausted"
	// ProviderErrorRateLimited is temporary pacing — the same call succeeds later.
	ProviderErrorRateLimited ProviderErrorKind = "provider_rate_limited"
	// ProviderErrorAuthFailed is a key the provider does not accept at all.
	ProviderErrorAuthFailed ProviderErrorKind = "provider_auth_failed"
)

// ProviderError is an upstream refusal carrying both the provider's own wording
// and our classification of it.
type ProviderError struct {
	Endpoint   string
	StatusCode int
	Message    string
	Kind       ProviderErrorKind
}

func (e *ProviderError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%s returned status %d", e.Endpoint, e.StatusCode)
	}
	return fmt.Sprintf("%s returned status %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

func newProviderError(endpoint string, status int, message string) *ProviderError {
	return &ProviderError{
		Endpoint:   endpoint,
		StatusCode: status,
		Message:    message,
		Kind:       classifyProviderError(status, message),
	}
}

// quotaPhrases mark a spent allowance rather than a bad key. Providers disagree
// on the status code — Mistral answers an exhausted quota with 401, OpenAI with
// 429, Anthropic with 400 — so the wording is the only reliable signal.
var quotaPhrases = []string{
	"usage limit",
	"limit reached",
	"limit exceeded",
	"exceeded your current quota",
	"quota",
	"insufficient_quota",
	"insufficient funds",
	"insufficient credit",
	"out of credit",
	"no credit",
	"credit balance",
	"billing",
	"inactive subscription",
	"subscription",
	"payment",
}

// rateLimitPhrases mark pacing, which passes on its own; quota does not.
var rateLimitPhrases = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"requests per",
	"slow down",
}

// classifyProviderError decides what an upstream refusal means for the user.
// Status code alone is not enough: a 401 is a dead key on most providers but an
// exhausted plan on Mistral, and only the message body separates the two.
func classifyProviderError(status int, message string) ProviderErrorKind {
	msg := strings.ToLower(message)
	hasQuota := containsAny(msg, quotaPhrases)
	hasRateLimit := containsAny(msg, rateLimitPhrases)

	switch {
	// Payment Required is unambiguous whatever the body says.
	case status == http.StatusPaymentRequired:
		return ProviderErrorQuotaExhausted
	// A 429 is pacing unless the body says the allowance itself is gone. The
	// rate-limit wording wins ties: "rate limit exceeded" contains "exceeded"
	// but is not a spent quota.
	case status == http.StatusTooManyRequests:
		if hasQuota && !hasRateLimit {
			return ProviderErrorQuotaExhausted
		}
		return ProviderErrorRateLimited
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		if hasQuota {
			return ProviderErrorQuotaExhausted
		}
		return ProviderErrorAuthFailed
	// Some providers report a spent balance as a plain 400.
	case hasQuota && status >= 400 && status < 500:
		return ProviderErrorQuotaExhausted
	default:
		return ProviderErrorUnknown
	}
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

const (
	// providerErrorBodyLimit caps how much of a failed response we read; error
	// payloads are a sentence, anything longer is an HTML error page.
	providerErrorBodyLimit = 4 << 10
	providerErrorMaxLen    = 300
)

// providerErrorMessage pulls the human-readable reason out of a failed provider
// response. Without it a status code alone is ambiguous: Mistral answers an
// exhausted quota with 401 (not 429), and only the body says so. Returns "" when
// the payload carries no usable message.
func providerErrorMessage(body []byte) string {
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return truncateMessage(msg)
		}
		if msg := strings.TrimSpace(payload.Detail); msg != "" {
			return truncateMessage(msg)
		}
		if len(payload.Error) > 0 {
			var nested struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(payload.Error, &nested); err == nil {
				if msg := strings.TrimSpace(nested.Message); msg != "" {
					return truncateMessage(msg)
				}
			}
			var plain string
			if err := json.Unmarshal(payload.Error, &plain); err == nil {
				if msg := strings.TrimSpace(plain); msg != "" {
					return truncateMessage(msg)
				}
			}
		}
		return ""
	}
	// Not JSON — a proxy or gateway answered. Its text still beats a bare code.
	return truncateMessage(strings.Join(strings.Fields(string(body)), " "))
}

func truncateMessage(msg string) string {
	if len(msg) <= providerErrorMaxLen {
		return msg
	}
	return msg[:providerErrorMaxLen] + "…"
}
