package llmretry_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The table is the point of this file: the agent loop and the four orchestrator
// stages now read their verdict from one function, so every decision it can make
// is pinned here rather than inferred from whichever caller happens to be under
// test. A row moving is a deliberate change to how every LLM call in the product
// behaves.
func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want llmretry.Action
	}{
		{
			name: "transport failure has no status and is exactly what a second attempt fixes",
			err:  errors.New("dial tcp: connection refused"),
			want: llmretry.Backoff,
		},
		{
			name: "wrapped transport failure is still a transport failure",
			err:  fmt.Errorf("chat: %w", errors.New("unexpected EOF")),
			want: llmretry.Backoff,
		},
		{
			name: "429 is the provider asking for a pause, not a refusal",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusTooManyRequests, Body: "slow down"},
			want: llmretry.Backoff,
		},
		{
			name: "408 request timeout",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusRequestTimeout},
			want: llmretry.Backoff,
		},
		{
			name: "409 conflict",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusConflict},
			want: llmretry.Backoff,
		},
		{
			name: "500 is the provider's problem and may be gone next second",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusInternalServerError, Body: "internal error"},
			want: llmretry.Backoff,
		},
		{
			name: "503 while shedding load",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusServiceUnavailable},
			want: llmretry.Backoff,
		},
		{
			name: "an LLMHTTPError wrapped by a caller is still classified on its status",
			err:  fmt.Errorf("planner: %w", &domain.LLMHTTPError{StatusCode: http.StatusBadGateway}),
			want: llmretry.Backoff,
		},
		{
			name: "400 for an unknown model gets the identical rejection every time",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusBadRequest, Body: "unknown model"},
			want: llmretry.Stop,
		},
		{
			name: "401 will not become authorised by being asked again",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusUnauthorized, Body: "invalid api key"},
			want: llmretry.Stop,
		},
		{
			name: "403 forbidden",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusForbidden},
			want: llmretry.Stop,
		},
		{
			name: "404 wrong endpoint",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusNotFound},
			want: llmretry.Stop,
		},
		{
			name: "422 without an overflow marker is a plain malformed request",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusUnprocessableEntity, Body: "tool_choice is invalid"},
			want: llmretry.Stop,
		},
		{
			name: "OpenAI-compatible overflow arrives as a 400",
			err: &domain.LLMHTTPError{
				StatusCode: http.StatusBadRequest,
				Body:       "This model's maximum context length is 8192 tokens",
			},
			want: llmretry.Shrink,
		},
		{
			name: "Anthropic says it in prose instead",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusBadRequest, Body: "prompt is too long"},
			want: llmretry.Shrink,
		},
		{
			name: "413 needs no body to be an overflow",
			err:  &domain.LLMHTTPError{StatusCode: http.StatusRequestEntityTooLarge},
			want: llmretry.Shrink,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, llmretry.Classify(context.Background(), tc.err),
				"classified as %s, expected %s", llmretry.Classify(context.Background(), tc.err), tc.want)
		})
	}
}

// A cancelled run outranks every status: the answer cannot be delivered anywhere
// even if the next attempt succeeds, and retrying inside a dead context only
// collects more context errors on top of the real cause.
func TestClassify_CancelledContextStopsWhateverTheErrorSays(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, err := range []error{
		errors.New("connection reset by peer"),
		&domain.LLMHTTPError{StatusCode: http.StatusTooManyRequests},
		&domain.LLMHTTPError{StatusCode: http.StatusRequestEntityTooLarge},
	} {
		assert.Equal(t, llmretry.Stop, llmretry.Classify(ctx, err))
	}
}

// The curve has to be able to outlast the provider's window. The old one —
// 500ms, 1s, 2s — spent every attempt inside the first second, which is exactly
// the window a free tier rate-limits on, so no attempt could ever have
// succeeded. Whole seconds is the point of these numbers.
func TestDelay_DoublesUpToTheCeiling(t *testing.T) {
	assert.Equal(t, 2*time.Second, llmretry.Delay(0))
	assert.Equal(t, 4*time.Second, llmretry.Delay(1))
	assert.Equal(t, 8*time.Second, llmretry.Delay(2))
	assert.Equal(t, llmretry.MaxDelay, llmretry.Delay(5))
	assert.Equal(t, llmretry.MaxDelay, llmretry.Delay(9), "the ceiling holds past the cap")
	assert.Equal(t, llmretry.MaxDelay, llmretry.Delay(64), "and past the point the shift overflows")

	assert.GreaterOrEqual(t, llmretry.Delay(0)+llmretry.Delay(1)+llmretry.Delay(2), 10*time.Second,
		"three attempts must span more than the one-second window that refused the first")
}

// A provider that sent Retry-After has told us when its window reopens, and
// that number beats anything we would have guessed.
func TestDelayFor_ObeysTheProvidersRetryAfter(t *testing.T) {
	err := &domain.LLMHTTPError{StatusCode: 429, RetryAfter: 12 * time.Second}
	assert.Equal(t, 12*time.Second, llmretry.DelayFor(err, 0))

	assert.Equal(t, llmretry.MaxDelay, llmretry.DelayFor(
		&domain.LLMHTTPError{StatusCode: 429, RetryAfter: time.Hour}, 0),
		"an absurd hint is clamped rather than hanging the run")
}

// Without a hint the curve applies, jittered: several runs and the indexer
// share one account, and a fixed schedule marches them back into the limit
// together.
func TestDelayFor_JittersTheCurveWhenTheProviderSaysNothing(t *testing.T) {
	err := &domain.LLMHTTPError{StatusCode: 429}
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		d := llmretry.DelayFor(err, 1)
		assert.GreaterOrEqual(t, d, llmretry.Delay(1)/2, "jitter must not collapse the wait")
		assert.LessOrEqual(t, d, llmretry.Delay(1))
		seen[d] = true
	}
	assert.Greater(t, len(seen), 1, "a fixed schedule is what synchronises callers into the same limit")
}

func TestAwait_WaitsThenAllowsAnotherAttemptOnATransientFailure(t *testing.T) {
	start := time.Now()
	err := llmretry.Await(context.Background(), &domain.LLMHTTPError{StatusCode: 429, RetryAfter: 600 * time.Millisecond}, 0, 2)

	require.NoError(t, err, "nil means try again")
	assert.GreaterOrEqual(t, time.Since(start), 500*time.Millisecond,
		"the wait the provider asked for has to actually happen")
}

// Await serves callers that assemble their prompt fresh from run facts, so an
// over-long request is terminal for them: there is no history to cut, and
// re-sending at exactly the length just refused is certainly useless.
func TestAwait_TreatsAnOverflowAsTerminal(t *testing.T) {
	cause := &domain.LLMHTTPError{StatusCode: 400, Body: "prompt is too long"}

	start := time.Now()
	err := llmretry.Await(context.Background(), cause, 0, 2)

	assert.Same(t, cause, err, "the caller must report the provider's own error")
	assert.Less(t, time.Since(start), 250*time.Millisecond, "a stop must not sleep on the way out")
}

func TestAwait_StopsWithoutSleepingOnANonRetryableRejection(t *testing.T) {
	cause := &domain.LLMHTTPError{StatusCode: 400, Body: "unknown model"}

	start := time.Now()
	err := llmretry.Await(context.Background(), cause, 0, 2)

	assert.Same(t, cause, err)
	assert.Less(t, time.Since(start), 250*time.Millisecond)
}

// The last attempt never sleeps: the caller is about to give up anyway, so a
// pause there is pure added latency on a failure that is already decided.
func TestAwait_DoesNotSleepOnTheFinalAttempt(t *testing.T) {
	cause := &domain.LLMHTTPError{StatusCode: 429}

	start := time.Now()
	err := llmretry.Await(context.Background(), cause, 2, 2)

	assert.Same(t, cause, err)
	assert.Less(t, time.Since(start), 250*time.Millisecond)
}

func TestAwait_ReportsTheProviderErrorWhenTheRunIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cause := errors.New("connection refused")

	start := time.Now()
	err := llmretry.Await(ctx, cause, 0, 2)

	assert.Same(t, cause, err, "the context error is not why the call failed")
	assert.Less(t, time.Since(start), 250*time.Millisecond)
}

// Cancelling mid-wait ends the retry rather than serving out the backoff, and
// still surfaces the provider failure the caller was already holding.
func TestAwait_GivesUpWhenTheRunIsCancelledDuringTheBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	cause := &domain.LLMHTTPError{StatusCode: 429}

	start := time.Now()
	err := llmretry.Await(ctx, cause, 0, 2)

	assert.Same(t, cause, err)
	assert.Less(t, time.Since(start), llmretry.BaseDelay, "it must not serve out the full wait")
}

// A refusal caused by the agent's engine being a local binary this host does not
// have is a CONFIGURATION fact, not a bad minute. Classify must say Stop, and
// this is the single place that decision is made for every pipeline stage.
//
// Without it the default below (Backoff, for anything that is not a classified
// HTTP error) sends the identical refusal three more times and delays the report
// by the whole backoff curve — and the report is the entire point, since the
// silent provider fallback that used to hide this has been removed.
func TestClassifyStopsOnAHostExecutedRefusal(t *testing.T) {
	direct := domain.ErrHostExecutedProvider(domain.LLMProviderClaudeCode)
	if got := llmretry.Classify(context.Background(), direct); got != llmretry.Stop {
		t.Fatalf("Classify(agentic refusal) = %v, want llmretry.Stop", got)
	}

	// Wrapped, which is how it actually arrives: every call site adds a sentence
	// of its own before the error reaches a retry loop.
	wrapped := fmt.Errorf("planner: %w", direct)
	if got := llmretry.Classify(context.Background(), wrapped); got != llmretry.Stop {
		t.Fatalf("Classify(wrapped refusal) = %v, want llmretry.Stop", got)
	}

	// And Await must not sleep on it, even with attempts left.
	if giveUp := llmretry.Await(context.Background(), wrapped, 0, 2); giveUp == nil {
		t.Fatal("Await returned nil: the caller would retry a refusal that can never succeed")
	}

	// The guard that this did not widen into "every unclassified error stops":
	// an ordinary transient failure must still back off.
	if got := llmretry.Classify(context.Background(), errors.New("connection reset by peer")); got != llmretry.Backoff {
		t.Fatalf("Classify(transient) = %v, want llmretry.Backoff — the retry policy must not have been broken", got)
	}
}
