package llmprovider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type ProviderErrorKind string

const (
	ProviderErrorUnknown ProviderErrorKind = ""

	ProviderErrorQuotaExhausted ProviderErrorKind = "provider_quota_exhausted"

	ProviderErrorRateLimited ProviderErrorKind = "provider_rate_limited"

	ProviderErrorAuthFailed ProviderErrorKind = "provider_auth_failed"
)

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

var rateLimitPhrases = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"requests per",
	"slow down",
}

func classifyProviderError(status int, message string) ProviderErrorKind {
	msg := strings.ToLower(message)
	hasQuota := containsAny(msg, quotaPhrases)
	hasRateLimit := containsAny(msg, rateLimitPhrases)

	switch {

	case status == http.StatusPaymentRequired:
		return ProviderErrorQuotaExhausted

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
	providerErrorBodyLimit = 4 << 10
	providerErrorMaxLen    = 300
)

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

	return truncateMessage(strings.Join(strings.Fields(string(body)), " "))
}

func truncateMessage(msg string) string {
	if len(msg) <= providerErrorMaxLen {
		return msg
	}
	return msg[:providerErrorMaxLen] + "…"
}
