package llmprovider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/require"
)

type mockEndpointStore struct {
	ep domain.LLMEndpoint
}

func (m *mockEndpointStore) List(ctx context.Context) ([]domain.LLMEndpoint, error) {
	return []domain.LLMEndpoint{m.ep}, nil
}

func (m *mockEndpointStore) Get(ctx context.Context, id string) (domain.LLMEndpoint, error) {
	if id != m.ep.ID {
		return domain.LLMEndpoint{}, context.Canceled
	}
	return m.ep, nil
}

func (m *mockEndpointStore) Create(ctx context.Context, ep domain.LLMEndpoint) (domain.LLMEndpoint, error) {
	m.ep = ep
	return ep, nil
}

func (m *mockEndpointStore) Update(ctx context.Context, ep domain.LLMEndpoint) error {
	m.ep = ep
	return nil
}

func (m *mockEndpointStore) Delete(ctx context.Context, id string) error { return nil }

func (m *mockEndpointStore) SetAPIKey(ctx context.Context, id string, encrypted []byte) error {
	return nil
}

func (m *mockEndpointStore) GetAPIKeyEncrypted(ctx context.Context, id string) ([]byte, error) {
	return nil, nil
}

func (m *mockEndpointStore) DeleteAPIKey(ctx context.Context, id string) error { return nil }

func newEmbeddingModelsService(t *testing.T, catalog string) (*llmprovider.Service, string) {
	return newEmbeddingModelsServiceWithStatus(t, http.StatusOK, catalog)
}

func newEmbeddingModelsServiceWithStatus(t *testing.T, status int, catalog string) (*llmprovider.Service, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(catalog))
	}))
	t.Cleanup(srv.Close)

	const id = "3f6bd8a2-0f8e-4a0e-9d2c-1c4f6d1f9e01"
	endpoints := &mockEndpointStore{ep: domain.LLMEndpoint{
		ID:         id,
		Name:       "Mistral",
		BaseURL:    srv.URL + "/v1",
		Configured: true,
	}}
	store := &mockStore{
		configs: map[domain.LLMProviderType]domain.LLMProviderConfig{},
		active:  domain.LLMProviderLocal,
		keys:    map[domain.LLMProviderType][]byte{},
	}
	return llmprovider.NewService(store, endpoints, nil, 0, nil), id
}

func TestListEmbeddingModelsForEndpointKeepsOnlyEmbeddingModels(t *testing.T) {
	svc, id := newEmbeddingModelsService(t, `{"data":[
		{"id":"mistral-large-latest"},
		{"id":"codestral-embed"},
		{"id":"mistral-small-latest"},
		{"id":"mistral-embed"}
	]}`)

	models, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderType(id))
	require.NoError(t, err)
	require.Equal(t, []string{"codestral-embed", "mistral-embed"}, models)
}

func TestListEmbeddingModelsForEndpointFallsBackToFullCatalog(t *testing.T) {
	svc, id := newEmbeddingModelsService(t, `{"data":[
		{"id":"nomic-text-v1.5"},
		{"id":"bge-m3"}
	]}`)

	models, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderType(id))
	require.NoError(t, err)
	require.Equal(t, []string{"nomic-text-v1.5", "bge-m3"}, models)
}

func TestListEmbeddingModelsSurfacesProviderErrorMessage(t *testing.T) {
	svc, id := newEmbeddingModelsServiceWithStatus(t, http.StatusUnauthorized,
		`{"message":"Inactive subscription or usage limit reached","request_id":"abc"}`)

	_, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderType(id))
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "Inactive subscription or usage limit reached")
}

func TestListEmbeddingModelsSurfacesNestedProviderErrorMessage(t *testing.T) {
	svc, id := newEmbeddingModelsServiceWithStatus(t, http.StatusTooManyRequests,
		`{"error":{"message":"You exceeded your current quota","type":"insufficient_quota"}}`)

	_, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderType(id))
	require.Error(t, err)
	require.Contains(t, err.Error(), "You exceeded your current quota")
}

func TestListEmbeddingModelsSurfacesNonJSONErrorBody(t *testing.T) {
	svc, id := newEmbeddingModelsServiceWithStatus(t, http.StatusBadGateway, "upstream connect error")

	_, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderType(id))
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream connect error")
}

func TestListEmbeddingModelsRejectsUnknownProviderRef(t *testing.T) {
	svc, _ := newEmbeddingModelsService(t, `{"data":[]}`)

	_, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderType("not-an-endpoint"))
	require.Error(t, err)
}
