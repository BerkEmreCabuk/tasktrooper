package opencode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The watcher must cancel the run on the first line that matches, and never
// again — a known opencode bug can leave the process printing indefinitely
// after the first 429, and calling an already-cancelled context's cancel
// func a second time is harmless but the fired-once guard is what keeps this
// test asserting the INTENDED behaviour rather than relying on that being
// harmless.
func TestRateLimitWatcherCancelsOnFirstMatchOnly(t *testing.T) {
	cancelled := 0
	w := &rateLimitWatcher{cancel: func() { cancelled++ }}

	n, err := w.Write([]byte("some ordinary log noise\n"))
	require.NoError(t, err)
	assert.Equal(t, 24, n)
	assert.Equal(t, 0, cancelled)

	_, err = w.Write([]byte(`ERROR service=llm error={"statusCode":429,"message":"rate limit"}`))
	require.NoError(t, err)
	assert.Equal(t, 1, cancelled)

	_, err = w.Write([]byte("more noise after the limit\n"))
	require.NoError(t, err)
	assert.Equal(t, 1, cancelled, "a fired watcher must not cancel a second time")
}

func TestQuotaBlockFrom(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		out      outcome
		stderr   string
		wantNone bool
	}{
		{
			name: "the error event's own name is a rate limit",
			out:  outcome{IsError: true, Status: "rate_limit_exceeded", Text: "Rate limit exceeded. Please try again later."},
		},
		{
			name: "the reported provider-relayed message is a rate limit",
			out:  outcome{IsError: true, Text: "Error from provider (Console): Rate limit exceeded. Please try again later."},
		},
		{
			name:   "the known-upstream-bug log line on stderr is a rate limit",
			out:    outcome{},
			stderr: `ERROR service=llm error={"statusCode":429,"message":"rate limit"}`,
		},
		{
			name:     "an unrelated failure is not a rate limit",
			out:      outcome{IsError: true, Text: "Error: ENOENT: no such file or directory, open 'go.mod'"},
			wantNone: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block := quotaBlockFrom(tc.out, tc.stderr, "sess-1", now)
			if tc.wantNone {
				assert.Nil(t, block, "only a rate limit may park a task")
				return
			}
			require.NotNil(t, block)
			assert.True(t, now.Add(domain.DefaultQuotaParkWindow).Equal(block.ResumeAt),
				"opencode never gives a reset time, so every park uses the default window")
			assert.Equal(t, "sess-1", block.CLISessionID)
			assert.Equal(t, domain.LLMProviderOpencode, block.Provider)
			assert.NotEmpty(t, block.Detail)
		})
	}
}
