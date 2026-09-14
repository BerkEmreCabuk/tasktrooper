package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestChatFailedErrorOnA429SpeaksToTheUserAndLeaksNothing(t *testing.T) {
	cause := &domain.LLMHTTPError{
		StatusCode: 429,
		Body:       `{"object":"error","message":"Rate limit exceeded","type":"rate_limited","param":null,"code":"1300","raw_status_code":429}`,
		Provider:   "openai",
		Model:      "gpt-4o",
	}
	err := &ChatFailedError{
		Stats: RunStats{Iterations: 3, ToolCalls: 2},
		Err:   fmt.Errorf("llm failed after 3 attempts: %w", cause),
	}

	got := err.Error()

	for _, leak := range []string{"raw_status_code", "1300", "429", "{", "llm chat failed", "attempts", "iterations"} {
		if strings.Contains(got, leak) {
			t.Errorf("the user-facing run error leaks %q:\n%s", leak, got)
		}
	}
	for _, want := range []string{"model provider", "provider: openai", "model: gpt-4o", "try again"} {
		if !strings.Contains(got, want) {
			t.Errorf("the user-facing run error is missing %q:\n%s", want, got)
		}
	}
}

func TestChatFailedErrorKeepsTheGenericPath(t *testing.T) {
	err := &ChatFailedError{
		Stats: RunStats{Iterations: 3},
		Err:   &domain.LLMHTTPError{StatusCode: 400, Body: "unknown model"},
	}
	if got := err.Error(); !strings.Contains(got, "llm chat failed") || !strings.Contains(got, "unknown model") {
		t.Errorf("a non-rate-limit failure lost its diagnostic wording: %s", got)
	}
}
