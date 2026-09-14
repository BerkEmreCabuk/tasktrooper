package llm

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestRateLimitHeadersCapturesLimitHeadersAndDropsSecrets(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	h.Set("X-RateLimit-Remaining-Requests", "0")
	h.Set("X-RateLimit-Reset-Tokens", "59s")
	h.Set("Ratelimit-Limit", "100")
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer sk-live-should-never-be-logged")
	h.Set("Set-Cookie", "session=abc")
	h.Set("X-Api-Key-Remaining", "0") // matches a limit marker AND is key-like

	got := rateLimitHeaders(h)

	require.Equal(t, "30", got["retry-after"])
	require.Equal(t, "0", got["x-ratelimit-remaining-requests"])
	require.Equal(t, "59s", got["x-ratelimit-reset-tokens"])
	require.Equal(t, "100", got["ratelimit-limit"])

	for _, forbidden := range []string{"authorization", "set-cookie", "x-api-key-remaining"} {
		require.NotContains(t, got, forbidden, "a key-like header was captured")
	}
	require.NotContains(t, got, "content-type", "an unrelated header was captured")
}

// The provider behind the incident sends nothing at all. An empty capture must
// be an ordinary result, not a panic or a nil map, because that emptiness is
// the evidence that the hedged user-facing wording is correct.
func TestRateLimitHeadersOnAProviderThatSendsNothing(t *testing.T) {
	require.Empty(t, rateLimitHeaders(http.Header{}))
	require.Empty(t, rateLimitHeaders(nil))
}

func TestHTTPErrorOnA429IsClassifiedAsARateLimit(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}
	body := []byte(`{"object":"error","message":"Rate limit exceeded","type":"rate_limited","code":"1300"}`)

	err := httpError(resp, body)

	require.True(t, err.RateLimited())
	require.False(t, err.QuotaExhausted(), "the provider never said the quota was spent")
	_, ok := domain.RateLimitOf(err)
	require.True(t, ok)
}
