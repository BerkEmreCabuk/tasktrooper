package llmprovider_test

// "Test connection" has to test the connection the operator actually has.
//
// It resolved its base URL as request-or-definition-default and never looked at
// the stored row, so a provider connected on http://127.0.0.1:11234/v1 was
// probed at http://127.0.0.1:1234/v1 — the "Custom (OpenAI-compatible)"
// default — and the connection refused there was reported as the user's. A
// green or red answer about an address nobody configured is worse than no
// answer, because somebody will act on it.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func newProviderStore() *mockStore {
	return &mockStore{
		configs: map[domain.LLMProviderType]domain.LLMProviderConfig{},
		keys:    map[domain.LLMProviderType][]byte{},
	}
}

// modelsServer stands in for an OpenAI-compatible endpoint and records the
// paths it was asked for.
func modelsServer(t *testing.T, paths *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"e2e-stub"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTestUsesTheStoredBaseURLNotTheDefinitionDefault(t *testing.T) {
	var paths []string
	srv := modelsServer(t, &paths)
	store := newProviderStore()
	store.configs[domain.LLMProviderLocal] = domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderLocal,
		BaseURL:      srv.URL + "/v1",
		DefaultModel: "e2e-stub",
		Configured:   true,
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	err := svc.Test(context.Background(), domain.LLMProviderLocal, domain.TestLLMProviderRequest{})

	require.NoError(t, err, "the stored endpoint answered; the definition's default would have been refused")
	require.Equal(t, []string{"/v1/models"}, paths)
}

// An explicit base_url is still what is tested — that is the "try this before I
// save it" case the form's button exists for.
func TestTestPrefersAnExplicitBaseURLOverTheStoredOne(t *testing.T) {
	var storedPaths, requestedPaths []string
	stored := modelsServer(t, &storedPaths)
	requested := modelsServer(t, &requestedPaths)
	store := newProviderStore()
	store.configs[domain.LLMProviderLocal] = domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderLocal,
		BaseURL:      stored.URL + "/v1",
		Configured:   true,
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	err := svc.Test(context.Background(), domain.LLMProviderLocal, domain.TestLLMProviderRequest{
		BaseURL: requested.URL + "/v1",
	})

	require.NoError(t, err)
	assert.Empty(t, storedPaths, "the stored endpoint was not probed")
	assert.Equal(t, []string{"/v1/models"}, requestedPaths)
}

func TestConnectStoresTheDefaultModelItWasGiven(t *testing.T) {
	var paths []string
	srv := modelsServer(t, &paths)
	store := newProviderStore()
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	_, err := svc.Connect(context.Background(), domain.LLMProviderLocal, domain.ConnectLLMProviderRequest{
		BaseURL:      srv.URL + "/v1",
		DefaultModel: "e2e-stub",
	})

	require.NoError(t, err)
	assert.Equal(t, "e2e-stub", store.configs[domain.LLMProviderLocal].DefaultModel,
		"the model the caller sent was stored, not silently dropped")
}

// The connect FORM does not ask for a model, so an omitted one must keep what
// is already configured rather than blank it.
func TestConnectKeepsTheStoredDefaultModelWhenNoneIsSent(t *testing.T) {
	var paths []string
	srv := modelsServer(t, &paths)
	store := newProviderStore()
	store.configs[domain.LLMProviderLocal] = domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderLocal,
		BaseURL:      srv.URL + "/v1",
		DefaultModel: "already-chosen",
		Configured:   true,
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	_, err := svc.Connect(context.Background(), domain.LLMProviderLocal, domain.ConnectLLMProviderRequest{
		BaseURL: srv.URL + "/v1",
	})

	require.NoError(t, err)
	assert.Equal(t, "already-chosen", store.configs[domain.LLMProviderLocal].DefaultModel)
}
