package llmretry

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Action int

const (
	Stop Action = iota

	Backoff

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
	BaseDelay = 2 * time.Second
	MaxDelay  = 60 * time.Second
)

func Classify(ctx context.Context, err error) Action {
	if ctx.Err() != nil {
		return Stop
	}

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

func Delay(attempt int) time.Duration {
	delay := BaseDelay << attempt
	if delay > MaxDelay || delay <= 0 {
		return MaxDelay
	}
	return delay
}

func DelayFor(err error, attempt int) time.Duration {
	if after := RetryAfterOf(err); after > 0 {
		if after > MaxDelay {
			return MaxDelay
		}
		return after
	}
	return jitter(Delay(attempt))
}

func RetryAfterOf(err error) time.Duration {
	var httpErr *domain.LLMHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.RetryAfter
	}
	return 0
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
}

func Wait(ctx context.Context, cause error, attempt int) error {
	return Sleep(ctx, DelayFor(cause, attempt))
}

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

func Await(ctx context.Context, cause error, attempt, maxRetries int) error {
	if attempt >= maxRetries {
		return cause
	}
	if Classify(ctx, cause) != Backoff {
		return cause
	}
	if err := Sleep(ctx, DelayFor(cause, attempt)); err != nil {

		return cause
	}
	return nil
}
