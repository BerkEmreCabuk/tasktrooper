package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The whole point of the type is that the three classes need opposite
// treatment. Getting a rate limit classified as fatal wastes a run; getting a
// malformed request classified as transient wastes the wall clock three times.
func TestLLMHTTPErrorRetryable(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{408, true},
		{409, true},
		{422, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
	}
	for _, tc := range cases {
		err := &domain.LLMHTTPError{StatusCode: tc.status}
		assert.Equal(t, tc.want, err.Retryable(), "status %d", tc.status)
	}
}

// An over-long conversation is the one failure a plain retry can never fix, and
// every provider words it differently.
func TestLLMHTTPErrorContextOverflow(t *testing.T) {
	overflow := []*domain.LLMHTTPError{
		{StatusCode: 400, Body: `{"error":{"message":"This model's maximum context length is 32768 tokens"}}`},
		{StatusCode: 400, Body: `{"error":"prompt is too long: 250000 tokens > 200000"}`},
		{StatusCode: 422, Body: "input is too long for this model"},
		{StatusCode: 413, Body: ""},
	}
	for _, err := range overflow {
		assert.True(t, err.ContextOverflow(), "expected overflow for %d: %s", err.StatusCode, err.Body)
	}

	notOverflow := []*domain.LLMHTTPError{
		{StatusCode: 400, Body: `{"error":"Unexpected tool call id None in tool message"}`},
		{StatusCode: 401, Body: "invalid api key"},
		{StatusCode: 429, Body: "rate limit exceeded"},
		{StatusCode: 500, Body: "internal error"},
	}
	for _, err := range notOverflow {
		assert.False(t, err.ContextOverflow(), "unexpected overflow for %d: %s", err.StatusCode, err.Body)
	}
}

// The wording predates the type; run summaries and task comments quote it.
func TestLLMHTTPErrorKeepsTheOriginalWording(t *testing.T) {
	err := domain.NewLLMHTTPError(429, []byte("slow down"))
	assert.Equal(t, "llm returned 429: slow down", err.Error())
}

// The body is quoted into comments and logs, and some providers echo the whole
// rejected request back.
func TestLLMHTTPErrorTruncatesTheBody(t *testing.T) {
	huge := make([]byte, 50_000)
	for i := range huge {
		huge[i] = 'x'
	}
	err := domain.NewLLMHTTPError(400, huge)
	assert.Less(t, len(err.Body), 3000)
}
