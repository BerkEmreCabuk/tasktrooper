package llmprovider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// invalidateCapture counts the invalidations a write triggers. It replaced a
// reloadCapture that recorded the entries PUSHED into a process-wide client —
// there is no push any more, so what a write does is say "this tenant changed"
// and what a test asserts on is what Resolve then answers.
type invalidateCapture struct{ calls int }

func (r *invalidateCapture) invalidate(context.Context) { r.calls++ }

func newControlPlaneStore() *mockStore {
	return &mockStore{
		configs: map[domain.LLMProviderType]domain.LLMProviderConfig{},
		active:  domain.LLMProviderLocal,
		keys:    map[domain.LLMProviderType][]byte{},
	}
}

// TestSetControlPlaneSynthesisesALocalRunnerEntry is the wiring point that
// keeps a cloud deployment's local runner client going through the SAME
// factory dispatch every other provider uses: SetControlPlane must not touch
// llm_provider_configs (nothing about the Mac tunnel is a stored tenant
// setting), only the entries a reload is handed.
func TestSetControlPlaneSynthesisesALocalRunnerEntry(t *testing.T) {
	store := newControlPlaneStore()
	capture := &invalidateCapture{}
	svc := llmprovider.NewService(store, nil, nil, 0, capture.invalidate)

	require.NoError(t, svc.SetControlPlane(context.Background(), "https://tasktrooper.ai", "signing-key"))
	require.Equal(t, 1, capture.calls, "wiring the control plane must invalidate the cached set")

	resolved, err := svc.Resolve(context.Background())
	require.NoError(t, err)
	var found *llmprovider.ProviderReloadEntry
	for i := range resolved.Entries {
		if resolved.Entries[i].ProviderType == domain.LLMProviderLocalRunner {
			found = &resolved.Entries[i]
		}
	}
	require.NotNil(t, found, "Resolve must include an entry for domain.LLMProviderLocalRunner")
	require.Equal(t, "https://tasktrooper.ai", found.BaseURL)
	require.Equal(t, "signing-key", found.APIKey)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, found.DefaultModel)

	// Nothing was written to the store: this is process-wide wiring, not a
	// tenant setting.
	_, ok := store.configs[domain.LLMProviderLocalRunner]
	require.False(t, ok)
}

// Without SetControlPlane, reload must never synthesise the entry — this is
// the self-hosted/desktop path, which has no control plane to reach.
func TestNoLocalRunnerEntryWithoutSetControlPlane(t *testing.T) {
	store := newControlPlaneStore()
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	resolved, err := svc.Resolve(context.Background())
	require.NoError(t, err)
	for _, e := range resolved.Entries {
		require.NotEqual(t, domain.LLMProviderLocalRunner, e.ProviderType)
	}
}

// TestResolvedEmbeddingDefaultsToThePinWhenAutoAndWired covers point 4 end to
// end at the service layer: a tenant that never called SetEmbedding is
// resolved onto the pinned local model the moment a control plane is wired,
// with no second stored setting and no change to the "" = auto convention.
func TestResolvedEmbeddingDefaultsToThePinWhenAutoAndWired(t *testing.T) {
	store := newControlPlaneStore()
	svc := llmprovider.NewService(store, nil, nil, 0, nil)
	require.NoError(t, svc.SetControlPlane(context.Background(), "https://tasktrooper.ai", "key"))

	model, dims, err := svc.ResolvedEmbedding(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, model)
	require.Equal(t, domain.PinnedLocalEmbeddingDimensions, dims)

	out, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.LLMProviderLocalRunner, out.EmbeddingProvider)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, out.EmbeddingModel)
}

// A tenant that explicitly chose a different embedding provider keeps that
// choice: SetControlPlane must never override a deliberate SetEmbedding.
func TestResolvedEmbeddingKeepsAnExplicitChoice(t *testing.T) {
	store := newControlPlaneStore()
	store.configs[domain.LLMProviderOpenAI] = domain.LLMProviderConfig{ProviderType: domain.LLMProviderOpenAI, Configured: true}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)
	require.NoError(t, svc.SetControlPlane(context.Background(), "https://tasktrooper.ai", "key"))

	_, err := svc.SetEmbedding(context.Background(), domain.LLMProviderOpenAI, "text-embedding-3-small")
	require.NoError(t, err)

	model, _, err := svc.ResolvedEmbedding(context.Background())
	require.NoError(t, err)
	require.Equal(t, "text-embedding-3-small", model)

	out, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.LLMProviderOpenAI, out.EmbeddingProvider)
}
