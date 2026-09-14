package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type LLMProviderStore interface {
	List(ctx context.Context) ([]domain.LLMProviderConfig, error)
	Get(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProviderConfig, error)
	Upsert(ctx context.Context, cfg domain.LLMProviderConfig) error
	SetAPIKey(ctx context.Context, providerType domain.LLMProviderType, encrypted []byte) error
	GetAPIKeyEncrypted(ctx context.Context, providerType domain.LLMProviderType) ([]byte, error)
	DeleteAPIKey(ctx context.Context, providerType domain.LLMProviderType) error
	GetActiveProvider(ctx context.Context) (domain.LLMProviderType, error)
	SetActiveProvider(ctx context.Context, providerType domain.LLMProviderType) error
	GetEmbeddingProvider(ctx context.Context) (domain.LLMProviderType, error)
	SetEmbeddingProvider(ctx context.Context, providerType domain.LLMProviderType) error
	GetEmbeddingModel(ctx context.Context) (string, error)
	SetEmbeddingModel(ctx context.Context, model string) error
}

// LLMEndpointStore persists named, multi-instance OpenAI-compatible endpoints.
// The endpoint ID (uuid) is the provider ref used everywhere a native provider
// type would be.
type LLMEndpointStore interface {
	List(ctx context.Context) ([]domain.LLMEndpoint, error)
	Get(ctx context.Context, id string) (domain.LLMEndpoint, error)
	Create(ctx context.Context, ep domain.LLMEndpoint) (domain.LLMEndpoint, error)
	Update(ctx context.Context, ep domain.LLMEndpoint) error
	Delete(ctx context.Context, id string) error
	SetAPIKey(ctx context.Context, id string, encrypted []byte) error
	GetAPIKeyEncrypted(ctx context.Context, id string) ([]byte, error)
	DeleteAPIKey(ctx context.Context, id string) error
}
