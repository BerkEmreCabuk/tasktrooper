package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// An endpoint ref (a uuid, not a declared provider type) must pass the guard, or no agent could ever be saved on a named OpenAI-compatible endpoint.
func TestAnEndpointRefIsNotTreatedAsUnavailable(t *testing.T) {
	store := &createStore{}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.CreateAgent(context.Background(), domain.CreateAgentRequest{
		Name:         "endpoint-builder",
		ProviderType: domain.LLMProviderType(uuid.New().String()),
		Model:        "some-model",
	})

	require.NoError(t, err)
	require.Equal(t, 1, store.calls)
}
