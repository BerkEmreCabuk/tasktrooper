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
	role := roleAgentDefinitions()[0].agent.Name
	store.agents = []domain.Agent{
		{ID: uuid.New(), Name: role, Enabled: true},
		{ID: uuid.New(), Name: "on-claude", ProviderType: domain.LLMProviderClaudeCode, Model: "sonnet", ModelHeavy: "opus", Enabled: true},
		{ID: uuid.New(), Name: "on-openai", ProviderType: domain.LLMProviderType("openai"), Model: "gpt-4o", Enabled: true},
		{ID: uuid.New(), Name: "custom-default", Enabled: true},
	}
	return store, NewService(store, stubLLMClient{}, ""), role
}

// Connecting only Cursor must give every agent that could not run a runtime that
// can: the role agent with no provider, and the agent stranded on Claude Code.
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
	if a := agentByName(t, store, "on-openai"); a.ProviderType != "openai" || a.Model != "gpt-4o" {
		t.Fatalf("an agent on an HTTP provider was moved: %+v", a)
	}
	if a := agentByName(t, store, "custom-default"); a.ProviderType != "" {
		t.Fatalf("a custom agent on the default provider was moved: %+v", a)
	}
}

// Claude Code wins a tie, with the model aliases the role prompts are tuned for;
// an agent already on a connected CLI keeps it.
func TestReconcileAgentRuntimes_PrefersClaudeCodeAndLeavesConnectedAgentsAlone(t *testing.T) {
	store, svc, role := runtimeFixture()

	moved, err := svc.ReconcileAgentRuntimes(context.Background(), []domain.LLMProviderType{
		domain.LLMProviderOpencode, domain.LLMProviderClaudeCode,
	})
	if err != nil {
		t.Fatalf("ReconcileAgentRuntimes: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved %d agents, want only the role agent", moved)
	}
	a := agentByName(t, store, role)
	if a.ProviderType != domain.LLMProviderClaudeCode || a.Model != "sonnet" || a.ModelHeavy != "opus" {
		t.Fatalf("role agent = %s/%q/%q, want claude_code sonnet/opus", a.ProviderType, a.Model, a.ModelHeavy)
	}
}

// With nothing connected there is nowhere better to send anyone.
func TestReconcileAgentRuntimes_MovesNothingWhenNoCLIIsConnected(t *testing.T) {
	store, svc, _ := runtimeFixture()

	moved, err := svc.ReconcileAgentRuntimes(context.Background(), nil)
	if err != nil || moved != 0 {
		t.Fatalf("moved %d (err %v), want 0", moved, err)
	}
	if a := agentByName(t, store, "on-claude"); a.ProviderType != domain.LLMProviderClaudeCode {
		t.Fatalf("an agent was moved with nothing connected: %+v", a)
	}
}
