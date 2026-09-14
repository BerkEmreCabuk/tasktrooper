package domain_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The exact body the incident produced: an OpenAI-compatible endpoint, no
// Retry-After, no reset header, a vendor-specific code. Nothing in it may reach
// a user.
const incidentBody = `{"object":"error","message":"Rate limit exceeded","type":"rate_limited","param":null,"code":"1300","raw_status_code":429}`

func TestRateLimitOfClassifiesA429AndKeepsTheBodyOutOfTheMessage(t *testing.T) {
	// Wrapped exactly as production wraps it: adapter -> loop -> run error.
	err := fmt.Errorf("llm chat failed: %w",
		fmt.Errorf("llm failed after 3 attempts: %w",
			&domain.LLMHTTPError{StatusCode: 429, Body: incidentBody, Provider: "openai", Model: "gpt-4o"}))

	rl, ok := domain.RateLimitOf(err)
	if !ok {
		t.Fatalf("a 429 from the provider was not classified as a rate limit")
	}
	if rl.Exhausted {
		t.Errorf("a body that only says %q must not be reported as quota exhausted", "Rate limit exceeded")
	}

	msg := rl.UserMessage()
	for _, leak := range []string{"raw_status_code", "1300", "429", "{", "llm chat failed", "attempts", "rate_limited"} {
		if strings.Contains(msg, leak) {
			t.Errorf("user-facing message leaks %q: %s", leak, msg)
		}
	}
	for _, want := range []string{"rate-limiting", "quota", "provider: openai", "model: gpt-4o", "try again"} {
		if !strings.Contains(msg, want) {
			t.Errorf("user-facing message is missing %q: %s", want, msg)
		}
	}
}

func TestRateLimitOfSeparatesExhaustedQuotaFromPacing(t *testing.T) {
	rl, ok := domain.RateLimitOf(&domain.LLMHTTPError{
		StatusCode: 429,
		Body:       `{"error":{"type":"insufficient_quota","message":"You exceeded your current quota"}}`,
	})
	if !ok || !rl.Exhausted {
		t.Fatalf("an insufficient_quota body must classify as exhausted (ok=%v exhausted=%v)", ok, rl.Exhausted)
	}
	if msg := rl.UserMessage(); !strings.Contains(msg, "Retrying will not help") {
		t.Errorf("an exhausted quota must say retrying will not help: %s", msg)
	}
	if msg := rl.UserMessage(); strings.Contains(msg, "(provider") || strings.Contains(msg, "(model") {
		t.Errorf("an unknown provider/model must be omitted, not invented: %s", msg)
	}
}

func TestRateLimitOfIgnoresFailuresThatAreNotLimits(t *testing.T) {
	for _, e := range []*domain.LLMHTTPError{
		{StatusCode: 400, Body: "unknown model"},
		{StatusCode: 401, Body: "invalid api key"},
		{StatusCode: 500, Body: "internal error"},
	} {
		if _, ok := domain.RateLimitOf(e); ok {
			t.Errorf("status %d was misclassified as a rate limit", e.StatusCode)
		}
	}
}
