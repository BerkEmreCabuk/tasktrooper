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

// createStore records the agent a successful save wrote.
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

// Saving an agent onto a provider whose engine is a local process, on a host
// with no runner attached, is refused at the moment the choice is made.
//
// The provider is only half of a working configuration; the other half is a
// process on this machine, and nothing on the agent record says whether one
// exists. Saved without it, that one choice produced about ten different
// runtime failures — a board run that names a missing binary, a chat turn that
// refuses, a verify-fix round that silently never happens — each describing its
// own symptom and none of them pointing here.
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
	// The sentinel is what lets the transport answer 400 rather than 500: this
	// is a choice to correct, not a server fault.
	require.ErrorIs(t, err, catalog.ErrNoHostRunner)
	require.Zero(t, store.calls, "the agent must not be persisted in a state nothing can run")
}

// With a runner attached, the same save goes through.
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

// Moving an EXISTING agent onto the provider is the same choice and gets the
// same answer — the update path is how most agents reach it.
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

// An ordinary HTTP provider never consults the probe: this guard is about a
// missing local process, not about provider configuration in general.
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
