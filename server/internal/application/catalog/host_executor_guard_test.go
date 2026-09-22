package catalog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type createStore struct {
	agentStore
	created domain.Agent
	calls   int
}

func (s *createStore) CreateAgent(_ context.Context, a domain.Agent) (domain.Agent, error) {
	s.calls++
	s.created = a
	return a, nil
}

func TestCreateAgentRefusesAHostExecutedProviderWithNoRunner(t *testing.T) {
	store := &createStore{}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.CreateAgent(context.Background(), domain.CreateAgentRequest{
		Name:         "builder",
		ProviderType: domain.LLMProviderClaudeCode,
		Model:        "opus",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "this workspace has no runner attached for it")
	require.Contains(t, err.Error(), "pick another provider")
	require.ErrorIs(t, err, catalog.ErrNoHostRunner)
	require.Zero(t, store.calls, "the agent must not be persisted in a state nothing can run")
}

func TestCreateAgentAllowsAHostExecutedProviderWhenARunnerIsAttached(t *testing.T) {
	store := &createStore{}
	svc := catalog.NewService(store, nil, "")
	svc.SetHostExecutorProbe(func(p domain.LLMProviderType) bool {
		return p == domain.LLMProviderClaudeCode
	})

	_, err := svc.CreateAgent(context.Background(), domain.CreateAgentRequest{
		Name:         "builder",
		ProviderType: domain.LLMProviderClaudeCode,
		Model:        "opus",
	})

	require.NoError(t, err)
	require.Equal(t, 1, store.calls)
	require.Equal(t, domain.LLMProviderClaudeCode, store.created.ProviderType)
}

func TestUpdateAgentRefusesAHostExecutedProviderWithNoRunner(t *testing.T) {
	id := uuid.New()
	store := &agentStore{existing: domain.Agent{ID: id, Name: "builder", ProviderType: domain.LLMProviderOpenAI}}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.UpdateAgent(context.Background(), id, domain.UpdateAgentRequest{
		Name:         "builder",
		ProviderType: domain.LLMProviderClaudeCode,
	})

	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "this workspace has no runner attached for it"))
	require.Empty(t, store.saved.Name, "the agent must not be persisted")
}

func TestSavingAnHTTPProviderIsUnaffected(t *testing.T) {
	store := &createStore{}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.CreateAgent(context.Background(), domain.CreateAgentRequest{
		Name:         "gpt-builder",
		ProviderType: domain.LLMProviderOpenAI,
		Model:        "gpt-4o",
	})

	require.NoError(t, err)
	require.Equal(t, 1, store.calls)
}
