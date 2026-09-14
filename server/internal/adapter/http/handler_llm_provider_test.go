package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeLLMProviderStore is an empty in-memory port.LLMProviderStore: the
// embedding-model tests below only exercise the named-endpoint path.
type fakeLLMProviderStore struct{}

var _ port.LLMProviderStore = (*fakeLLMProviderStore)(nil)

func (fakeLLMProviderStore) List(context.Context) ([]domain.LLMProviderConfig, error) {
	return nil, nil
}

func (fakeLLMProviderStore) Get(context.Context, domain.LLMProviderType) (domain.LLMProviderConfig, error) {
	return domain.LLMProviderConfig{}, nil
}

func (fakeLLMProviderStore) Upsert(context.Context, domain.LLMProviderConfig) error { return nil }

func (fakeLLMProviderStore) SetAPIKey(context.Context, domain.LLMProviderType, []byte) error {
	return nil
}

func (fakeLLMProviderStore) GetAPIKeyEncrypted(context.Context, domain.LLMProviderType) ([]byte, error) {
	return nil, nil
}

func (fakeLLMProviderStore) DeleteAPIKey(context.Context, domain.LLMProviderType) error { return nil }

func (fakeLLMProviderStore) GetActiveProvider(context.Context) (domain.LLMProviderType, error) {
	return domain.LLMProviderLocal, nil
}

func (fakeLLMProviderStore) SetActiveProvider(context.Context, domain.LLMProviderType) error {
	return nil
}

func (fakeLLMProviderStore) GetEmbeddingProvider(context.Context) (domain.LLMProviderType, error) {
	return "", nil
}

func (fakeLLMProviderStore) SetEmbeddingProvider(context.Context, domain.LLMProviderType) error {
	return nil
}

func (fakeLLMProviderStore) GetEmbeddingModel(context.Context) (string, error) { return "", nil }

func (fakeLLMProviderStore) SetEmbeddingModel(context.Context, string) error { return nil }

// fakeLLMEndpointStore holds one named endpoint keyed by its uuid.
type fakeLLMEndpointStore struct{ ep domain.LLMEndpoint }

var _ port.LLMEndpointStore = (*fakeLLMEndpointStore)(nil)

func (f *fakeLLMEndpointStore) List(context.Context) ([]domain.LLMEndpoint, error) {
	return []domain.LLMEndpoint{f.ep}, nil
}

func (f *fakeLLMEndpointStore) Get(_ context.Context, id string) (domain.LLMEndpoint, error) {
	if id != f.ep.ID {
		return domain.LLMEndpoint{}, port.ErrNotFound
	}
	return f.ep, nil
}

func (f *fakeLLMEndpointStore) Create(_ context.Context, ep domain.LLMEndpoint) (domain.LLMEndpoint, error) {
	f.ep = ep
	return ep, nil
}

func (f *fakeLLMEndpointStore) Update(_ context.Context, ep domain.LLMEndpoint) error {
	f.ep = ep
	return nil
}

func (f *fakeLLMEndpointStore) Delete(context.Context, string) error { return nil }

func (f *fakeLLMEndpointStore) SetAPIKey(context.Context, string, []byte) error { return nil }

func (f *fakeLLMEndpointStore) GetAPIKeyEncrypted(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (f *fakeLLMEndpointStore) DeleteAPIKey(context.Context, string) error { return nil }

// A named endpoint is referenced by its uuid, not by a native provider type.
// Rejecting every non-native ref left the embedding-model dropdown empty for
// every endpoint the user configured (Mistral, OpenRouter, …).
func TestListEmbeddingModelsAcceptsNamedEndpointRef(t *testing.T) {
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(nethttp.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"mistral-large-latest"},{"id":"codestral-embed"}]}`))
	}))
	defer upstream.Close()

	const id = "8a1f0d3e-58b2-4f5a-9c77-2a1b3c4d5e6f"
	endpoints := &fakeLLMEndpointStore{ep: domain.LLMEndpoint{
		ID:         id,
		Name:       "Mistral",
		BaseURL:    upstream.URL + "/v1",
		Configured: true,
	}}
	h := &Handler{llmProviderSvc: llmprovider.NewService(fakeLLMProviderStore{}, endpoints, nil, 0, nil)}
	app := fiber.New()
	h.registerLLMProviderRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/llm/embedding-models?provider="+id, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for a named endpoint ref, got %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Models) != 1 || out.Models[0] != "codestral-embed" {
		t.Fatalf("expected the endpoint's embedding models, got %v", out.Models)
	}
}

func TestListEmbeddingModelsRejectsEmptyProviderRef(t *testing.T) {
	h := &Handler{llmProviderSvc: llmprovider.NewService(fakeLLMProviderStore{}, &fakeLLMEndpointStore{}, nil, 0, nil)}
	app := fiber.New()
	h.registerLLMProviderRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/llm/embedding-models", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 without a provider, got %d", resp.StatusCode)
	}
}
