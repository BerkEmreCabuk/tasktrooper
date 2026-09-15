package catalog

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// cliPreference orders the agent CLIs when several are connected. The role
// agents' prompts and model aliases are tuned for Claude Code, so it wins a tie.
var cliPreference = []domain.LLMProviderType{
	domain.LLMProviderClaudeCode,
	domain.LLMProviderCursorAgent,
	domain.LLMProviderAntigravity,
	domain.LLMProviderOpencode,
}

// ReconcileAgentRuntimes points agents at a CLI that can run them after the set
// of connected CLIs changes, and returns how many it moved.
//
// An agent on a CLI that is not connected moves to a connected one, and a role
// agent with no provider adopts one. Agents on an HTTP provider, and custom
// agents left on the default provider, are someone's choice and stay put. With
// no CLI connected nothing moves: an agent still on a disconnected CLI fails
// with "connect it", which says what to do, where the default provider would
// fail with something vaguer.
func (s *Service) ReconcileAgentRuntimes(ctx context.Context, connected []domain.LLMProviderType) (int, error) {
	target := preferredCLI(connected)
	if target == "" {
		return 0, nil
	}
	isConnected := make(map[domain.LLMProviderType]bool, len(connected))
	for _, p := range connected {
		isConnected[p] = true
	}
	roles := make(map[string]bool)
	for _, def := range roleAgentDefinitions() {
		roles[def.agent.Name] = true
	}

	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	moved := 0
	for _, agent := range agents {
		adopt := agent.ProviderType == "" && roles[agent.Name]
		stranded := domain.RequiresHostExecutor(agent.ProviderType) && !isConnected[agent.ProviderType]
		if !adopt && !stranded {
			continue
		}
		agent.ProviderType = target
		agent.Model, agent.ModelHeavy = defaultCLIModels(target)
		if _, err := s.store.UpdateAgent(ctx, agent); err != nil {
			return moved, fmt.Errorf("update agent %s: %w", agent.Name, err)
		}
		moved++
	}
	return moved, nil
}

func preferredCLI(connected []domain.LLMProviderType) domain.LLMProviderType {
	for _, p := range cliPreference {
		for _, c := range connected {
			if c == p {
				return p
			}
		}
	}
	return ""
}

// defaultCLIModels is sonnet/opus on Claude Code and the CLI's own default
// everywhere else: a model name is only valid for the provider it was picked
// from.
func defaultCLIModels(p domain.LLMProviderType) (model, heavy string) {
	if p == roleAgentProvider {
		return roleAgentModel, roleAgentModelHeavy
	}
	return "", ""
}
