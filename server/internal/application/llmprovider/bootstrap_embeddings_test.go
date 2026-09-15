package llmprovider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const embedderURL = "http://127.0.0.1:51545"

// A fresh install: the OpenAI-compatible slot points at the bundled embedder
// and is chosen for embeddings, with nobody opening the settings page.
func TestBootstrapEmbeddings_ConfiguresTheBundledEmbedderOnAFreshInstall(t *testing.T) {
	store := newProviderStore()
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	require.NoError(t, svc.BootstrapEmbeddings(context.Background(), embedderURL))

	row := store.configs[domain.LLMProviderLocal]
	require.True(t, row.Configured)
	require.Equal(t, embedderURL, row.BaseURL)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, row.DefaultModel)
	require.Equal(t, domain.LLMProviderLocal, store.embeddingProvider)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, store.embeddingModel)
}

// The desktop picks a new port every launch, so a row that is ours follows it.
func TestBootstrapEmbeddings_FollowsTheEmbedderToItsNewPort(t *testing.T) {
	store := newProviderStore()
	store.configs[domain.LLMProviderLocal] = domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderLocal,
		BaseURL:      "http://127.0.0.1:40000",
		DefaultModel: domain.PinnedLocalEmbeddingModel,
		Configured:   true,
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	require.NoError(t, svc.BootstrapEmbeddings(context.Background(), embedderURL))

	require.Equal(t, embedderURL, store.configs[domain.LLMProviderLocal].BaseURL)
}

// Someone connected their own OpenAI-compatible endpoint for chat. Rewriting its
// base URL would send their chat model to the embedder, so neither the row nor
// the embedding choice is touched.
func TestBootstrapEmbeddings_LeavesAUserConfiguredEndpointAlone(t *testing.T) {
	store := newProviderStore()
	mine := domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderLocal,
		BaseURL:      "http://192.168.1.20:11434/v1",
		DefaultModel: "qwen2.5-coder:32b",
		Configured:   true,
	}
	store.configs[domain.LLMProviderLocal] = mine
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	require.NoError(t, svc.BootstrapEmbeddings(context.Background(), embedderURL))

	require.Equal(t, mine, store.configs[domain.LLMProviderLocal])
	require.Empty(t, store.embeddingProvider)
}
