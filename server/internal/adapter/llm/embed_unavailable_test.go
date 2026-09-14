package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// embedServer answers every /embeddings call with one status and body.
func embedServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEmbedTypesTheRefusalsNoRetryCanClear is the incident: Mistral answered 402
// with a subscription link, the string reached the model verbatim, and
// codebase_search was called eight times in one run. The status has to survive
// as a type for the tool to say anything better than the provider's own prose.
func TestEmbedTypesTheRefusalsNoRetryCanClear(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
	} {
		srv := embedServer(t, status, `{"detail":"Check your subscription on https://admin.mistral.ai/subscription"}`)
		client := NewOpenAICompatClient(srv.URL, "embed-model", "key", 5*time.Second)

		_, err := client.Embed(context.Background(), "anything", "embed-model")
		require.Error(t, err)

		var unavailable *EmbeddingUnavailableError
		require.True(t, errors.As(err, &unavailable), "status %d was not typed", status)
		require.Equal(t, status, unavailable.StatusCode)
		// The provider's own wording stays on the error, so logs and stored run
		// summaries read the same as before the type existed.
		require.Contains(t, unavailable.Error(), "admin.mistral.ai")
	}
}

// A 429 must stay a *RateLimitError: it is the one refusal that reopens on its
// own, and the account limiter needs the type to honour Retry-After.
func TestEmbedKeepsRateLimitsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer srv.Close()

	client := NewOpenAICompatClient(srv.URL, "embed-model", "key", 5*time.Second)
	_, err := client.Embed(context.Background(), "anything", "embed-model")

	var limited *RateLimitError
	require.True(t, errors.As(err, &limited))
	require.Equal(t, 7*time.Second, limited.RetryAfter)

	var unavailable *EmbeddingUnavailableError
	require.False(t, errors.As(err, &unavailable))
}

// A plain server fault is neither: retrying it can work, and nothing downstream
// should tell the agent to stop using semantic search over one 500.
func TestEmbedLeavesOtherFailuresUntyped(t *testing.T) {
	srv := embedServer(t, http.StatusInternalServerError, "boom")
	client := NewOpenAICompatClient(srv.URL, "embed-model", "key", 5*time.Second)

	_, err := client.Embed(context.Background(), "anything", "embed-model")
	require.Error(t, err)

	var unavailable *EmbeddingUnavailableError
	require.False(t, errors.As(err, &unavailable))
	var limited *RateLimitError
	require.False(t, errors.As(err, &limited))
}

// A provider that cannot be reached at all is the same problem for the caller
// as one that refuses: there is no embedding, and no amount of asking again in
// this run produces one.
func TestEmbedTypesAnUnreachableProvider(t *testing.T) {
	srv := embedServer(t, http.StatusOK, "{}")
	url := srv.URL
	srv.Close()

	client := NewOpenAICompatClient(url, "embed-model", "key", 2*time.Second)
	_, err := client.Embed(context.Background(), "anything", "embed-model")
	require.Error(t, err)

	var unavailable *EmbeddingUnavailableError
	require.True(t, errors.As(err, &unavailable))
	require.Zero(t, unavailable.StatusCode)
	require.NotNil(t, unavailable.Cause)
}

// A caller that walked away is not the provider being down. Reporting our own
// cancellation as an unreachable provider would tell the agent to abandon
// semantic search for the rest of a run it had itself interrupted.
func TestEmbedDoesNotBlameTheProviderForOurCancellation(t *testing.T) {
	srv := embedServer(t, http.StatusOK, "{}")
	client := NewOpenAICompatClient(srv.URL, "embed-model", "key", 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Embed(ctx, "anything", "embed-model")
	require.Error(t, err)

	var unavailable *EmbeddingUnavailableError
	require.False(t, errors.As(err, &unavailable))
}

// ----------------------------------------------------------------- gemini

// geminiEmbedClientAgainst points the Gemini SDK at a local server, so the
// classification below is exercised through the real SDK error types rather
// than hand-built ones — the whole risk on this path is that the SDK reports
// a refusal in a shape this code does not recognise.
func geminiEmbedClientAgainst(t *testing.T, handler http.HandlerFunc) *geminiVertexClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return geminiEmbedClientAt(t, srv.URL)
}

func geminiEmbedClientAt(t *testing.T, baseURL string) *geminiVertexClient {
	t.Helper()
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:      "key",
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{BaseURL: baseURL},
	})
	require.NoError(t, err)
	return &geminiVertexClient{client: client, model: "gemini-embedding-001"}
}

// geminiError writes the error envelope Gemini and Vertex both return.
func geminiError(status int, code int, message, rpcStatus string, details string) http.HandlerFunc {
	body := fmt.Sprintf(`{"error":{"code":%d,"message":%q,"status":%q%s}}`, code, message, rpcStatus, details)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// The incident was a Mistral tenant, but a Gemini/Vertex tenant hit the same
// wall with none of the protection: its refusals came back as
// `gemini embed: Error 403, Message: ...`, so codebase_search had nothing to
// match on and kept telling the agent, in the provider's words, to try again.
func TestGeminiEmbedTypesTheRefusalsNoRetryCanClear(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
	} {
		client := geminiEmbedClientAgainst(t, geminiError(status, status,
			"Vertex AI API has not been used in project 1234 before or it is disabled.",
			"PERMISSION_DENIED", ""))

		_, err := client.Embed(context.Background(), "anything", "gemini-embedding-001")
		require.Error(t, err)

		var unavailable *EmbeddingUnavailableError
		require.True(t, errors.As(err, &unavailable), "status %d was not typed", status)
		require.Equal(t, status, unavailable.StatusCode)
		// The provider's own wording stays on the error for the logs.
		require.Contains(t, unavailable.Error(), "has not been used in project")
	}
}

// A 429 stays retryable here too, and carries the wait Gemini states in its
// RetryInfo detail — the SDK drops response headers, so that detail is the only
// place the number survives.
func TestGeminiEmbedKeepsRateLimitsRetryable(t *testing.T) {
	client := geminiEmbedClientAgainst(t, geminiError(
		http.StatusTooManyRequests, http.StatusTooManyRequests,
		"Quota exceeded for quota metric 'Embed content requests'.", "RESOURCE_EXHAUSTED",
		`,"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"23s"}]`))

	_, err := client.Embed(context.Background(), "anything", "gemini-embedding-001")

	var limited *RateLimitError
	require.True(t, errors.As(err, &limited))
	require.Equal(t, http.StatusTooManyRequests, limited.StatusCode)
	require.Equal(t, 23*time.Second, limited.RetryAfter)

	var unavailable *EmbeddingUnavailableError
	require.False(t, errors.As(err, &unavailable))
}

// A server fault is neither, on this client for the same reason as the other:
// retrying it can work.
func TestGeminiEmbedLeavesOtherFailuresUntyped(t *testing.T) {
	client := geminiEmbedClientAgainst(t, geminiError(
		http.StatusInternalServerError, http.StatusInternalServerError, "boom", "INTERNAL", ""))

	_, err := client.Embed(context.Background(), "anything", "gemini-embedding-001")
	require.Error(t, err)

	var unavailable *EmbeddingUnavailableError
	require.False(t, errors.As(err, &unavailable))
	var limited *RateLimitError
	require.False(t, errors.As(err, &limited))
}

func TestGeminiEmbedTypesAnUnreachableProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := geminiEmbedClientAt(t, url)
	_, err := client.Embed(context.Background(), "anything", "gemini-embedding-001")
	require.Error(t, err)

	var unavailable *EmbeddingUnavailableError
	require.True(t, errors.As(err, &unavailable))
	require.Zero(t, unavailable.StatusCode)
	require.NotNil(t, unavailable.Cause)
}

// Our own cancellation is not the provider being down here either.
func TestGeminiEmbedDoesNotBlameTheProviderForOurCancellation(t *testing.T) {
	client := geminiEmbedClientAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Embed(ctx, "anything", "gemini-embedding-001")
	require.Error(t, err)

	var unavailable *EmbeddingUnavailableError
	require.False(t, errors.As(err, &unavailable))
}
