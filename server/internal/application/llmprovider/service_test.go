package llmprovider_test

import (
	"context"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/require"
)

type mockStore struct {
	configs map[domain.LLMProviderType]domain.LLMProviderConfig
	active  domain.LLMProviderType
	keys    map[domain.LLMProviderType][]byte

	// embeddingProvider/embeddingModel are stateful so ResolvedEmbedding tests
	// can assert on what SetEmbedding actually stored.
	embeddingProvider domain.LLMProviderType
	embeddingModel    string
}

func (m *mockStore) List(ctx context.Context) ([]domain.LLMProviderConfig, error) {
	out := make([]domain.LLMProviderConfig, 0, len(m.configs))
	for _, cfg := range m.configs {
		out = append(out, cfg)
	}
	return out, nil
}

func (m *mockStore) Get(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProviderConfig, error) {
	cfg, ok := m.configs[providerType]
	if !ok {
		return domain.LLMProviderConfig{}, context.Canceled
	}
	return cfg, nil
}

func (m *mockStore) Upsert(ctx context.Context, cfg domain.LLMProviderConfig) error {
	m.configs[cfg.ProviderType] = cfg
	return nil
}

func (m *mockStore) SetAPIKey(ctx context.Context, providerType domain.LLMProviderType, encrypted []byte) error {
	m.keys[providerType] = encrypted
	return nil
}

func (m *mockStore) GetAPIKeyEncrypted(ctx context.Context, providerType domain.LLMProviderType) ([]byte, error) {
	return m.keys[providerType], nil
}

func (m *mockStore) DeleteAPIKey(ctx context.Context, providerType domain.LLMProviderType) error {
	delete(m.keys, providerType)
	return nil
}

func (m *mockStore) GetActiveProvider(ctx context.Context) (domain.LLMProviderType, error) {
	return m.active, nil
}

func (m *mockStore) SetActiveProvider(ctx context.Context, providerType domain.LLMProviderType) error {
	m.active = providerType
	return nil
}

func (m *mockStore) GetEmbeddingProvider(ctx context.Context) (domain.LLMProviderType, error) {
	return m.embeddingProvider, nil
}

func (m *mockStore) SetEmbeddingProvider(ctx context.Context, providerType domain.LLMProviderType) error {
	m.embeddingProvider = providerType
	return nil
}

func (m *mockStore) GetEmbeddingModel(ctx context.Context) (string, error) {
	return m.embeddingModel, nil
}

func (m *mockStore) SetEmbeddingModel(ctx context.Context, model string) error {
	m.embeddingModel = model
	return nil
}

func TestConnectOpenAIRequiresAPIKey(t *testing.T) {
	store := &mockStore{
		configs: map[domain.LLMProviderType]domain.LLMProviderConfig{},
		active:  domain.LLMProviderLocal,
		keys:    map[domain.LLMProviderType][]byte{},
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	_, err := svc.Connect(context.Background(), domain.LLMProviderOpenAI, domain.ConnectLLMProviderRequest{
		BaseURL: "https://api.openai.com/v1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}

func TestListReturnsAllProviders(t *testing.T) {
	store := &mockStore{
		configs: map[domain.LLMProviderType]domain.LLMProviderConfig{
			domain.LLMProviderLocal: {
				ProviderType: domain.LLMProviderLocal,
				BaseURL:      "http://127.0.0.1:1234/v1",
				Configured:   true,
			},
		},
		active: domain.LLMProviderLocal,
		keys:   map[domain.LLMProviderType][]byte{},
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	out, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, out.Providers, len(domain.AllLLMProviderDefinitions()),
		"the list is every definition, including the host-executed ones an agent can select but nobody connects")
	require.Equal(t, domain.LLMProviderLocal, out.ActiveProvider)
}

// A host-executed provider is listed so an agent can be put on it, but it is
// not something to dial: connecting, testing or making it the default
// would store a configured row for an endpoint that does not exist — and, for
// Activate, break every chat turn with a missing-client error instead of one
// honest sentence here.
func TestHostExecutedProviderCannotBeConnectedOrActivated(t *testing.T) {
	store := &mockStore{
		configs: map[domain.LLMProviderType]domain.LLMProviderConfig{},
		active:  domain.LLMProviderLocal,
		keys:    map[domain.LLMProviderType][]byte{},
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)
	ctx := context.Background()

	// The sentinel, not the prose: it is what the transport turns into a 409
	// (adapter/http/permanent_refusal.go), so it is the part of this refusal
	// that other code depends on.
	_, err := svc.Connect(ctx, domain.LLMProviderClaudeCode, domain.ConnectLLMProviderRequest{})
	require.ErrorIs(t, err, domain.ErrHostExecutedUnservable)
	require.NotContains(t, err.Error(), "on the server host",
		"in cloud this provider runs on the member's Mac, not where this server runs")

	_, err = svc.Activate(ctx, domain.LLMProviderClaudeCode)
	require.ErrorIs(t, err, domain.ErrHostExecutedUnservable)

	require.ErrorIs(t, svc.Test(ctx, domain.LLMProviderClaudeCode, domain.TestLLMProviderRequest{}),
		domain.ErrHostExecutedUnservable)

	_, err = svc.SetEmbedding(ctx, domain.LLMProviderClaudeCode, "whatever")
	require.ErrorContains(t, err, "cannot produce embeddings")

	require.Equal(t, domain.LLMProviderLocal, store.active, "a rejected activate must not have moved the default")
}
