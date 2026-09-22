package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Answers the two agent lookups UpdateAgent performs; the embedded interface leaves the rest unimplemented.
type agentStore struct {
	port.CatalogStore
	existing domain.Agent
	saved    domain.Agent
}

func (s *agentStore) GetAgent(_ context.Context, _ uuid.UUID) (domain.Agent, error) {
	return s.existing, nil
}

func (s *agentStore) UpdateAgent(_ context.Context, agent domain.Agent) (domain.Agent, error) {
	s.saved = agent
	return agent, nil
}

func TestUpdateAgent_ProviderSwitchDropsTheOldProvidersModels(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{
		ID:           id,
		Name:         "product-manager",
		ProviderType: "anthropic",
		Model:        "anthropic/claude-sonnet-5",
		ModelHeavy:   "anthropic/claude-opus-5",
	}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, domain.UpdateAgentRequest{
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "anthropic/claude-sonnet-5",
		ModelHeavy:   "anthropic/claude-opus-5",
	})

	require.NoError(t, err)
	assert.Empty(t, store.saved.Model)
	assert.Empty(t, store.saved.ModelHeavy)
}

func TestUpdateAgent_ProviderSwitchKeepsTheModelsPickedForTheNewProvider(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{
		ID:           id,
		Name:         "product-manager",
		ProviderType: "anthropic",
		Model:        "anthropic/claude-sonnet-5",
		ModelHeavy:   "anthropic/claude-opus-5",
	}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, domain.UpdateAgentRequest{
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "mistral-small-latest",
		ModelHeavy:   "mistral-large-latest",
	})

	require.NoError(t, err)
	assert.Equal(t, "mistral-small-latest", store.saved.Model)
	assert.Equal(t, "mistral-large-latest", store.saved.ModelHeavy)
}

func TestUpdateAgent_SameProviderKeepsTheModels(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{
		ID:           id,
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "mistral-small-latest",
		ModelHeavy:   "mistral-large-latest",
	}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, domain.UpdateAgentRequest{
		Name:         "product-manager",
		ProviderType: "mistral-endpoint",
		Model:        "mistral-small-latest",
		ModelHeavy:   "mistral-large-latest",
	})

	require.NoError(t, err)
	assert.Equal(t, "mistral-small-latest", store.saved.Model)
	assert.Equal(t, "mistral-large-latest", store.saved.ModelHeavy)
}
