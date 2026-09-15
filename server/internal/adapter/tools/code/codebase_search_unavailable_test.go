package code_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// searchToolAgainst builds codebase_search over a completed index whose only
// moving part is the embedding client, so what the tool says about a refusal is
// the only thing under test.
func searchToolAgainst(t *testing.T, client port.LLMClient) (port.ToolExecutor, context.Context) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "application", "mapper", "testdata", "sample"))
	require.NoError(t, err)

	sessionID := uuid.New()
	sid := sessionID
	store := &fakeIndexStore{index: domain.WorkspaceIndex{
		ID:        uuid.New(),
		SessionID: &sid,
		RootPath:  root,
		Status:    domain.IndexStatusCompleted,
	}}

	kit := code.NewToolKit(
		store,
		client,
		mapper.NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50}),
		domain.IndexerConfig{TopK: 3},
		domain.GraphConfig{MaxExpansionDepth: 2, MaxExpandedChunks: 8},
		"embed-model",
	)

	ctx := registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), sessionID),
		root,
	)
	return code.NewExecutors(kit)[0], ctx
}

func embeddingsAnswering(t *testing.T, status int, body string) port.LLMClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return llm.NewOpenAICompatClient(srv.URL, "embed-model", "key", 5*time.Second)
}

// TestCodebaseSearchOnEmbeddingRejection is the production transcript: the tool
// answered `embed query: embeddings returned 402: {"detail":"Check your
// subscription on https://admin.mistral.ai/subscription"}` eight times in one
// run because nothing in it told the agent to stop. The replacement has to name
// the tools that still work and say not to come back.
func TestCodebaseSearchOnEmbeddingRejection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"unauthorized", http.StatusUnauthorized},
		{"payment required", http.StatusPaymentRequired},
		{"forbidden", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool, ctx := searchToolAgainst(t, embeddingsAnswering(t, tc.status,
				`{"detail":"Check your subscription on https://admin.mistral.ai/subscription"}`))

			result := tool.Execute(ctx, `{"query":"entry point"}`)

			require.True(t, result.IsError)
			require.Contains(t, result.Content, "semantic search is unavailable on this server")
			require.Contains(t, result.Content, "embedding provider rejected the request: HTTP")
			require.Contains(t, result.Content, "do not call codebase_search again in this run")
			// The three tools that still work on an unindexed-search server.
			require.Contains(t, result.Content, "grep_code")
			require.Contains(t, result.Content, "get_repo_tree")
			require.Contains(t, result.Content, "read_file")
			// The provider's own prose is what the agent used to act on; it
			// belongs in the log, not in the model's context.
			require.NotContains(t, result.Content, "admin.mistral.ai")
		})
	}
}

// A provider that could not be reached is a different verdict: a DNS blip, a
// TLS reset or a client timeout clears on its own, so saying "do not call this
// again in this run" would cost the run its semantic search over a hiccup. The
// answer offers one retry and names the fallbacks for if that fails too.
func TestCodebaseSearchOnUnreachableEmbeddingProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	tool, ctx := searchToolAgainst(t, llm.NewOpenAICompatClient(url, "embed-model", "key", 2*time.Second))
	result := tool.Execute(ctx, `{"query":"entry point"}`)

	require.True(t, result.IsError)
	require.Contains(t, result.Content, "embedding provider could not be reached")
	require.Contains(t, result.Content, "Retry codebase_search once after a moment")
	require.Contains(t, result.Content, "grep_code")
	require.NotContains(t, result.Content, "do not call codebase_search again in this run")
}

// A 429 is the other recoverable one: the window reopens, so the tool says how
// long the provider asked for and allows a single retry rather than writing
// semantic search off for the run.
func TestCodebaseSearchOnRateLimitedEmbeddingProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	t.Cleanup(srv.Close)

	tool, ctx := searchToolAgainst(t, llm.NewOpenAICompatClient(srv.URL, "embed-model", "key", 5*time.Second))
	result := tool.Execute(ctx, `{"query":"entry point"}`)

	require.True(t, result.IsError)
	require.Contains(t, result.Content, "embedding provider is rate-limited (HTTP 429, retry after 7s)")
	require.Contains(t, result.Content, "at most one retry")
	require.Contains(t, result.Content, "grep_code")
	require.NotContains(t, result.Content, "do not call codebase_search again in this run")
}

// A server fault is not a reason to give up on semantic search for the rest of
// the run, so it keeps the ordinary wording and no instruction to stop.
func TestCodebaseSearchKeepsOrdinaryWordingForAServerFault(t *testing.T) {
	tool, ctx := searchToolAgainst(t, embeddingsAnswering(t, http.StatusInternalServerError, "boom"))

	result := tool.Execute(ctx, `{"query":"entry point"}`)

	require.True(t, result.IsError)
	require.Contains(t, result.Content, "embed query:")
	require.NotContains(t, result.Content, "do not call codebase_search again in this run")
}

// The missing-index message predates this change and answers a different
// question ("no index" vs "no embeddings"); it must not be swallowed by it.
func TestCodebaseSearchStillReportsAMissingIndex(t *testing.T) {
	kit := code.NewToolKit(
		&fakeIndexStore{},
		&fakeLLM{embedding: []float32{0.1, 0.2}},
		mapper.NewService(domain.MappingConfig{Enabled: true}),
		domain.IndexerConfig{TopK: 3},
		domain.GraphConfig{},
		"embed-model",
	)
	ctx := registry.ContextWithSessionID(context.Background(), uuid.New())

	result := code.NewExecutors(kit)[0].Execute(ctx, `{"query":"entry point"}`)

	require.True(t, result.IsError)
	require.Contains(t, result.Content, "this repository has no semantic index yet")
}
