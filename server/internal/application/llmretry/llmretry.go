// Package llmretry holds the one answer to "can another attempt fix this failed
// provider call, and how long do we wait first".
//
// It exists because there were two answers. The agent loop classified failures
// into wait / shrink / stop; the orchestrator pipeline restated the same policy
// inline, only because the agent's version was an unexported method it could not
// call. Two copies of a retry policy are one policy plus a countdown to the day
// they disagree: domain.LLMHTTPError.Retryable() has to gain a single status
// code for the pipeline to start hammering a provider the agent loop already
// backs off from — silently, in production, on whichever stage happens to hit it
// first.
package llmretry

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Action is what can actually fix a failed provider call. The callers used to
// have one answer for all of them — send it again, immediately, three times —
// which is the right move for exactly one of the three.
type Action int

const (
	// Stop: the provider rejected the request itself. The same bytes get the
	// same rejection, so further attempts only delay the failure.
	Stop Action = iota
	// Backoff: transient. A reset connection, a timeout, a rate limit — worth
	// sending again, but only after waiting.
	Backoff
	// Shrink: the conversation is longer than the model accepts. Retrying it
	// unchanged cannot work; retrying it smaller is the only thing that can.
	// Callers that have nothing to shrink must treat this as terminal — see
	// Await.
	Shrink
)

func (a Action) String() string {
	switch a {
	case Backoff:
		return "backoff"
	case Shrink:
		return "shrink"
	default:
		return "stop"
	}
}

const (
	// BaseDelay and MaxDelay describe one backoff curve for every caller. They
	// are deliberately not configurable: the number that matters here is the
	// provider's patience, not ours, and a per-caller knob is how the two
	// implementations drifted in the first place.
	//
	// The old curve was 500ms/1s/2s under an 8s ceiling, which meant three
	// attempts finished inside about a second. Against a provider whose free
	// tier allows roughly one request per second per account, every one of them
	// landed inside the window that had just refused the first — the retry
	// budget was spent without ever waiting long enough to be allowed through.
	// A curve that can outlast a per-second window has to be measured in
	// seconds, not milliseconds.
	BaseDelay = 2 * time.Second
	MaxDelay  = 60 * time.Second
)

// Classify decides what to do about a failed provider call.
//
// A cancelled context is checked first and always stops: the run is over, and
// retrying inside a dead context just collects more context errors, which is
// what used to bury the real cause in the logs.
//
// Anything that is not a typed HTTP rejection reached here from the transport —
// dial failures, resets, read timeouts, truncated bodies — and those are the
// failures a second attempt genuinely fixes, so an unrecognised error backs off
// rather than stopping. That asymmetry is on purpose: mistaking a transient
// failure for a permanent one kills a run that would have succeeded, while the
// reverse costs one extra request.
func Classify(ctx context.Context, err error) Action {
	if ctx.Err() != nil {
		return Stop
	}
	// A request naming a provider this host cannot serve is refused before any
	// endpoint is touched, so there is no endpoint whose mood could change. It
	// is a configuration fact, not a transient failure, and the default below
	// (Backoff, for anything that is not a classified HTTP error) would send
	// the same refusal three more times and delay the report by the whole
	// backoff curve.
	//
	// It is checked HERE rather than at each call site because every pipeline
	// stage that can raise it sits inside its own retry loop, and one policy in
	// one place is the reason this package exists at all.
	if errors.Is(err, domain.ErrHostExecutedUnservable) {
		return Stop
	}
	var httpErr *domain.LLMHTTPError
	if errors.As(err, &httpErr) {
		switch {
		case httpErr.ContextOverflow():
			return Shrink
		case httpErr.Retryable():
			return Backoff
		default:
			return Stop
		}
	}
	return Backoff
}

// Delay doubles per attempt (0-based) up to a ceiling. It is the base curve,
// without jitter, and stays exported and deterministic so the schedule can be
// asserted; callers should reach for Wait, which adds the two things that make
// the curve work in production.
func Delay(attempt int) time.Duration {
	delay := BaseDelay << attempt
	if delay > MaxDelay || delay <= 0 { // shift overflow on an absurd attempt count
		return MaxDelay
	}
	return delay
}

// DelayFor is the wait before re-sending a specific failure.
//
// A provider that sent Retry-After has told us when its window reopens, and no
// curve we invent can beat that number: it is used as-is (clamped to the
// ceiling, so a provider asking for an hour does not hang a run). Only when the
// provider said nothing does the exponential curve apply, and then with jitter
// — several runs and the background indexer share one account, and a fixed
// schedule marches all of them back into the limit in the same instant.
func DelayFor(err error, attempt int) time.Duration {
	if after := RetryAfterOf(err); after > 0 {
		if after > MaxDelay {
			return MaxDelay
		}
		return after
	}
	return jitter(Delay(attempt))
}

// RetryAfterOf reports the wait the provider asked for, zero when it did not.
func RetryAfterOf(err error) time.Duration {
	var httpErr *domain.LLMHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.RetryAfter
	}
	return 0
}

// jitter keeps at least half the curve and spreads the rest, so callers that
// were refused together do not return together.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
}

// Wait sleeps for as long as this failure deserves, giving up if the run is
// cancelled. It is the between-attempts wait for callers that drive their own
// retry loop, such as the agent loop, which can also shrink instead of resend.
func Wait(ctx context.Context, cause error, attempt int) error {
	return Sleep(ctx, DelayFor(cause, attempt))
}

// Sleep waits, but gives up the moment the run is cancelled.
func Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Await is the whole between-attempts decision for a caller that can only
// re-send or give up. nil means "try again", and the wait has already happened
// by the time it returns; a non-nil error means no further attempt can help, so
// the caller reports cause now instead of spending the attempts it has left.
//
// Shrink is terminal here, and that is the one place this differs from the agent
// loop. The agent owns a conversation it can cut down and re-send; a caller
// reaching for Await is assembling its prompt fresh from facts it needs in full,
// with no history budget to trim. Re-sending at exactly the length that was just
// refused is the one outcome that is certainly useless, so it stops instead.
//
// attempt is 0-based and maxRetries is the number of retries allowed after the
// first attempt, so the last attempt never sleeps on its way out — the caller is
// about to give up anyway, and a pause there only adds latency to a failure that
// is already decided.
func Await(ctx context.Context, cause error, attempt, maxRetries int) error {
	if attempt >= maxRetries {
		return cause
	}
	if Classify(ctx, cause) != Backoff {
		return cause
	}
	if err := Sleep(ctx, DelayFor(cause, attempt)); err != nil {
		// The run died mid-wait. Report the provider failure, not the context
		// error: the former is why the caller is here.
		return cause
	}
	return nil
}
