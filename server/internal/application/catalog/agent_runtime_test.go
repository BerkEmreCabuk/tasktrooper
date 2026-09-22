package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func agentByName(t *testing.T, store *memCatalogStore, name string) domain.Agent {
	t.Helper()
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	for _, a := range agents {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no agent named %s", name)
	return domain.Agent{}
}

func runtimeFixture() (*memCatalogStore, *Service, string) {
	store := newMemCatalogStore()
	role := "system-architect"
	store.agents = []domain.Agent{
		{ID: uuid.New(), Name: role, CatalogSlug: role, Enabled: true},
		{ID: uuid.New(), Name: "on-claude", ProviderType: domain.LLMProviderClaudeCode, Model: "sonnet", ModelHeavy: "opus", Enabled: true},
		{ID: uuid.New(), Name: "on-openai", ProviderType: domain.LLMProviderOpenAI, Model: "gpt-4o", Enabled: true},
		{ID: uuid.New(), Name: "custom-default", Enabled: true},
	}
	return store, NewService(store, stubLLMClient{}, ""), role
}

func TestReconcileAgentRuntimes_MovesAgentsThatCannotRunOntoTheConnectedCLI(t *testing.T) {
	store, svc, role := runtimeFixture()

	moved, err := svc.ReconcileAgentRuntimes(context.Background(), []domain.LLMProviderType{domain.LLMProviderCursorAgent})
	if err != nil {
		t.Fatalf("ReconcileAgentRuntimes: %v", err)
	}
	if moved != 2 {
		t.Fatalf("moved %d agents, want 2", moved)
	}
	for _, name := range []string{role, "on-claude"} {
		a := agentByName(t, store, name)
		if a.ProviderType != domain.LLMProviderCursorAgent || a.Model != "" || a.ModelHeavy != "" {
			t.Fatalf("%s = %s/%q/%q, want cursor_agent with the CLI's own models", name, a.ProviderType, a.Model, a.ModelHeavy)
		}
	}
	if a := agentByName(t, store, "on-openai"); a.ProviderType != domain.LLMProviderOpenAI || a.Model != "gpt-4o" {
		t.Fatalf("an agent on an HTTP provider was moved: %+v", a)
	}
	if a := agentByName(t, store, "custom-default"); a.ProviderType != "" {
		t.Fatalf("a custom agent on the default provider was moved: %+v", a)
	}
}

func TestReconcileAgentRuntimes_PrefersClaudeCodeAndLeavesConnectedAgentsAlone(t *testing.T) {
	store, svc, role := runtimeFixture()

	moved, err := svc.ReconcileAgentRuntimes(context.Background(), []domain.LLMProviderType{
		domain.LLMProviderOpencode, domain.LLMProviderClaudeCode,
	})
	if err != nil {
		t.Fatalf("ReconcileAgentRuntimes: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved %d agents, want only the catalog agent", moved)
	}
	a := agentByName(t, store, role)
	if a.ProviderType != domain.LLMProviderClaudeCode {
		t.Fatalf("catalog agent = %s, want claude_code (CLI default model)", a.ProviderType)
	}
}

func TestReconcileAgentRuntimes_FallsBackToActiveHTTPProviderWithItsModels(t *testing.T) {
	store, svc, role := runtimeFixture()
	svc.SetLLMProviders(stubProviderStore{
		active: domain.LLMProviderAnthropic,
		cfgs: map[domain.LLMProviderType]domain.LLMProviderConfig{
			domain.LLMProviderAnthropic: {ProviderType: domain.LLMProviderAnthropic, Configured: true},
		},
	})

	moved, err := svc.ReconcileAgentRuntimes(context.Background(), nil)
	if err != nil {
		t.Fatalf("ReconcileAgentRuntimes: %v", err)
	}
	if moved != 2 {
		t.Fatalf("moved %d agents, want 2", moved)
	}
	for _, name := range []string{role, "on-claude"} {
		a := agentByName(t, store, name)
		if a.ProviderType != domain.LLMProviderAnthropic || a.Model != "claude-sonnet-5" || a.ModelHeavy != "claude-opus-5" {
			t.Fatalf("%s = %s/%q/%q, want anthropic with its own model pair", name, a.ProviderType, a.Model, a.ModelHeavy)
		}
	}
}

func TestReconcileAgentRuntimes_IgnoresUnconfiguredActiveHTTPProvider(t *testing.T) {
	store, svc, _ := runtimeFixture()
	svc.SetLLMProviders(stubProviderStore{
		active: domain.LLMProviderAnthropic,
		cfgs:   map[domain.LLMProviderType]domain.LLMProviderConfig{},
	})

	moved, err := svc.ReconcileAgentRuntimes(context.Background(), nil)
	if err != nil || moved != 0 {
		t.Fatalf("moved %d (err %v), want 0", moved, err)
	}
	if a := agentByName(t, store, "on-claude"); a.ProviderType != domain.LLMProviderClaudeCode {
		t.Fatalf("an agent was moved onto an unconfigured provider: %+v", a)
	}
}

func TestReconcileAgentRuntimes_MovesNothingWithNoUsableProvider(t *testing.T) {
	store, svc, _ := runtimeFixture()

	moved, err := svc.ReconcileAgentRuntimes(context.Background(), nil)
	if err != nil || moved != 0 {
		t.Fatalf("moved %d (err %v), want 0", moved, err)
	}
	if a := agentByName(t, store, "on-claude"); a.ProviderType != domain.LLMProviderClaudeCode {
		t.Fatalf("an agent was moved with nothing connected: %+v", a)
	}
}

type stubProviderStore struct {
	active domain.LLMProviderType
	cfgs   map[domain.LLMProviderType]domain.LLMProviderConfig
}

func (s stubProviderStore) GetActiveProvider(context.Context) (domain.LLMProviderType, error) {
	return s.active, nil
}

func (s stubProviderStore) Get(_ context.Context, t domain.LLMProviderType) (domain.LLMProviderConfig, error) {
	return s.cfgs[t], nil
}
