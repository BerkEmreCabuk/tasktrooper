package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/require"
)

func TestParseRetryAfterSeconds(t *testing.T) {
	require.Equal(t, 5*time.Second, parseRetryAfter("5"))
	require.Equal(t, time.Duration(0), parseRetryAfter(""))
	require.Equal(t, time.Duration(0), parseRetryAfter("0"))
	require.Equal(t, time.Duration(0), parseRetryAfter("garbage"))
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	when := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(when)
	require.Greater(t, got, 20*time.Second)
	require.LessOrEqual(t, got, 31*time.Second)

	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	require.Equal(t, time.Duration(0), parseRetryAfter(past))
}

func TestIsRateLimited(t *testing.T) {
	require.False(t, isRateLimited(nil))
	require.False(t, isRateLimited(errors.New("embeddings returned 500: boom")))
	require.True(t, isRateLimited(&RateLimitError{Endpoint: "embeddings", StatusCode: 429}))
	// The proxy in front of the embedding model reports the limit only in the body.
	require.True(t, isRateLimited(fmt.Errorf(`embeddings returned 429: {"type":"rate_limited"}`)))
	require.True(t, isRateLimited(fmt.Errorf("wrapped: %w", errors.New("RESOURCE_EXHAUSTED"))))
}

func TestEmbedLimiterRetriesRateLimitedCall(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{MaxRetries: 3, RetryBackoff: time.Millisecond, MaxRetryWait: 5 * time.Millisecond})

	calls := 0
	vec, err := l.do(context.Background(), func(context.Context) ([]float32, error) {
		calls++
		if calls < 3 {
			return nil, &RateLimitError{Endpoint: "embeddings", StatusCode: 429, Body: "slow down"}
		}
		return []float32{1, 2}, nil
	})

	require.NoError(t, err)
	require.Equal(t, []float32{1, 2}, vec)
	require.Equal(t, 3, calls)
}

func TestEmbedLimiterGivesUpAfterMaxRetries(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{MaxRetries: 2, RetryBackoff: time.Millisecond, MaxRetryWait: time.Millisecond})

	calls := 0
	_, err := l.do(context.Background(), func(context.Context) ([]float32, error) {
		calls++
		return nil, &RateLimitError{Endpoint: "embeddings", StatusCode: 429, Body: "slow down"}
	})

	require.Error(t, err)
	require.Equal(t, 3, calls) // first attempt + 2 retries
	var rle *RateLimitError
	require.True(t, errors.As(err, &rle))
}

func TestEmbedLimiterDoesNotRetryOtherErrors(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{MaxRetries: 5, RetryBackoff: time.Millisecond})

	calls := 0
	_, err := l.do(context.Background(), func(context.Context) ([]float32, error) {
		calls++
		return nil, errors.New("embeddings returned 400: bad model")
	})

	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func TestEmbedLimiterSpacesCalls(t *testing.T) {
	var l accountLimiter
	// 6000/min = one call every 10ms.
	l.configure(domain.EmbeddingConfig{RequestsPerMinute: 6000})

	start := time.Now()
	for i := 0; i < 3; i++ {
		_, err := l.do(context.Background(), func(context.Context) ([]float32, error) {
			return []float32{0}, nil
		})
		require.NoError(t, err)
	}
	// First call runs immediately, the next two each wait out the interval.
	require.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

func TestEmbedLimiterHonoursRetryAfterForFollowingCalls(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{MaxRetries: 0, MaxRetryWait: time.Minute})

	_, err := l.do(context.Background(), func(context.Context) ([]float32, error) {
		return nil, &RateLimitError{Endpoint: "embeddings", StatusCode: 429, RetryAfter: 40 * time.Millisecond}
	})
	require.Error(t, err)

	// The penalty applies to the next caller too, not only to the retry.
	start := time.Now()
	_, err = l.do(context.Background(), func(context.Context) ([]float32, error) {
		return []float32{0}, nil
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

func TestEmbedLimiterRespectsContextCancellation(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{RequestsPerMinute: 60}) // one per second

	_, err := l.do(context.Background(), func(context.Context) ([]float32, error) {
		return []float32{0}, nil
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = l.do(ctx, func(context.Context) ([]float32, error) {
		return []float32{0}, nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestNewRateLimitErrorOnlyForRetryableStatuses(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"7"}}}
	rle := newRateLimitError("embeddings", resp, "slow down")
	require.NotNil(t, rle)
	require.Equal(t, 7*time.Second, rle.RetryAfter)
	require.Equal(t, "embeddings returned 429: slow down", rle.Error())

	require.Nil(t, newRateLimitError("embeddings", &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}}, "bad"))
}

// The bug this limiter exists to prevent: chat and embedding spend the same
// provider account, so a chat call must consume a slot in the same window the
// indexer is filling. Before this, chat was invisible to the counter and walked
// into a 429 on its first attempt with nobody else on the system.
func TestAccountLimiterPacesChatAgainstTheSameWindowAsEmbedding(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{RequestsPerMinute: 240}) // one call per 250ms

	ctx := context.Background()
	_, err := l.do(ctx, func(context.Context) ([]float32, error) { return []float32{1}, nil })
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, l.guard(ctx, "openai", "gpt-4o", func(context.Context) error { return nil }))
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond,
		"a chat call must wait its turn in the account's window, not slip past the counter")
}

// A 429 on the chat path has to slow everything on that account down — the
// embedding stream included — or the next call walks straight back into it.
func TestAccountLimiterChatRateLimitHoldsBackTheWholeAccount(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{})

	ctx := context.Background()
	err := l.guard(ctx, "openai", "gpt-4o", func(context.Context) error {
		return &domain.LLMHTTPError{StatusCode: 429, RetryAfter: 300 * time.Millisecond}
	})
	require.Error(t, err)

	start := time.Now()
	_, embedErr := l.do(ctx, func(context.Context) ([]float32, error) { return []float32{1}, nil })
	require.NoError(t, embedErr)
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond,
		"the wait the provider asked for on the chat call must hold the embedding stream too")
}

// Background work yields: a reindex that is already waiting must not take the
// slot ahead of chat requests that arrive while it waits.
func TestAccountLimiterBackgroundCallsYieldToInteractiveOnes(t *testing.T) {
	var l accountLimiter
	l.configure(domain.EmbeddingConfig{RequestsPerMinute: 600}) // one call per 100ms

	// Occupy the current slot so both callers below have to wait.
	require.NoError(t, l.guard(context.Background(), "openai", "gpt-4o", func(context.Context) error { return nil }))

	order := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		bg := domain.WithBackgroundLLM(context.Background())
		if _, err := l.do(bg, func(context.Context) ([]float32, error) { return []float32{1}, nil }); err != nil {
			return
		}
		order <- "background"
	}()

	// Give the background caller time to start waiting, then send four
	// interactive calls; every one of them must be served first.
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < 4; i++ {
		require.NoError(t, l.guard(context.Background(), "openai", "gpt-4o", func(context.Context) error { return nil }))
	}
	order <- "interactive"

	<-done
	require.Equal(t, "interactive", <-order,
		"a person waiting on a reply must not queue behind a reindex nobody asked for")
}

// stubClient is a provider that answers instantly, so what the test measures is
// the pacing the MultiProviderClient imposes and nothing else.
type stubClient struct{}

func (stubClient) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (stubClient) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (stubClient) Models(context.Context) ([]string, error) { return nil, nil }

func (stubClient) Embed(context.Context, string, string) ([]float32, error) {
	return []float32{1}, nil
}

// End to end through the router: one account means one limiter, so an embedding
// call and a chat call to the same provider queue behind each other.
func TestMultiProviderClientChatSharesTheEmbeddingAccountsLimiter(t *testing.T) {
	m := NewMultiProviderClient(nil, StaticResolver(staticSet("mistral",
		map[domain.LLMProviderType]port.LLMClient{"mistral": stubClient{}})))
	m.SetEmbeddingLimits(domain.EmbeddingConfig{RequestsPerMinute: 240}) // one call per 250ms

	ctx := context.Background()
	_, err := m.Embed(ctx, "hello", "embed-model")
	require.NoError(t, err)

	start := time.Now()
	_, err = m.Chat(ctx, domain.AgentRequest{ProviderType: "mistral"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond,
		"chat spends the same account's quota as the indexer, so it must share its counter")
}

// A provider that does not serve embeddings has its own, unrelated quota:
// pacing it with the embedding budget would throttle chat for no reason.
func TestMultiProviderClientDoesNotPaceAnUnrelatedAccount(t *testing.T) {
	m := NewMultiProviderClient(nil, StaticResolver(staticSet("mistral",
		map[domain.LLMProviderType]port.LLMClient{"mistral": stubClient{}, "anthropic": stubClient{}})))
	m.SetEmbeddingLimits(domain.EmbeddingConfig{RequestsPerMinute: 60}) // one call per second

	ctx := context.Background()
	_, err := m.Embed(ctx, "hello", "embed-model")
	require.NoError(t, err)

	start := time.Now()
	_, err = m.Chat(ctx, domain.AgentRequest{ProviderType: "anthropic"})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 200*time.Millisecond)
}

func TestHTTPErrorKeepsTheProvidersRetryAfter(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set("Retry-After", "7")

	err := httpError(resp, []byte(`{"type":"rate_limited"}`))
	require.Equal(t, 7*time.Second, err.RetryAfter)
	require.Equal(t, 7*time.Second, retryAfterOf(err),
		"the chat path must surface the header the embedding path already obeyed")
}

// A provider that refuses a call does not have to say so with Retry-After.
// OpenAI-compatible ones (and the proxies in front of them) state the same fact
// as a reset countdown on the rate-limit headers, and some answer in
// milliseconds. Reading only Retry-After throws that away and falls back to a
// curve we invented, which is how a chat run spends all three attempts inside a
// window the provider had already told us the length of.
func TestRetryHintReadsTheProvidersResetHeaders(t *testing.T) {
	hint := func(kv ...string) time.Duration {
		h := http.Header{}
		for i := 0; i+1 < len(kv); i += 2 {
			h.Set(kv[i], kv[i+1])
		}
		return retryHint(h)
	}

	// Retry-After stays authoritative when it is there.
	require.Equal(t, 7*time.Second, hint("Retry-After", "7", "X-RateLimit-Reset-Requests", "60s"))
	// Millisecond form, used by several OpenAI-compatible gateways.
	require.Equal(t, 1500*time.Millisecond, hint("Retry-After-Ms", "1500"))
	// Go-style durations are the common shape of the reset headers.
	require.Equal(t, 2*time.Minute+59*time.Second+560*time.Millisecond,
		hint("X-RateLimit-Reset-Tokens", "2m59.56s"))
	// Both present: the longer window is the one that is still shut.
	require.Equal(t, 30*time.Second,
		hint("X-RateLimit-Reset-Requests", "1.5s", "X-RateLimit-Reset-Tokens", "30s"))
	// A bare number on a reset header means seconds.
	require.Equal(t, 12*time.Second, hint("X-RateLimit-Reset-Requests", "12"))
	// Nothing to go on.
	require.Equal(t, time.Duration(0), hint("X-RateLimit-Remaining-Requests", "0"))
}

func TestHTTPErrorKeepsTheProvidersResetHint(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Reset-Requests", "8.5s")

	err := httpError(resp, []byte(`{"object":"error","type":"rate_limited","code":"1300"}`))
	require.Equal(t, 8500*time.Millisecond, err.RetryAfter,
		"a 429 that states its own reset must not be retried on our guessed curve")
	require.Equal(t, 8500*time.Millisecond, retryAfterOf(err))
}
