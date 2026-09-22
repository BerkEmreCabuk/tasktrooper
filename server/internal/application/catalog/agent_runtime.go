package catalog

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The provider store slice the reconciler reads: which provider is active and whether it is configured.
type LLMProviders interface {
	GetActiveProvider(ctx context.Context) (domain.LLMProviderType, error)
	Get(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProviderConfig, error)
}

// Declaration order breaks ties among connected CLIs; a new CLI joins by being declared.
var cliPreference = func() []domain.LLMProviderType {
	var out []domain.LLMProviderType
	for _, def := range domain.AllLLMProviderDefinitions() {
		if def.HostExecuted {
			out = append(out, def.Type)
		}
	}
	return out
}()

// Target is the best connected CLI, else the install's configured active HTTP provider; agents on disconnected CLIs move, custom agents on their default stay put, and nothing moves when nothing usable exists.
func (s *Service) ReconcileAgentRuntimes(ctx context.Context, connected []domain.LLMProviderType) (int, error) {
	target := preferredCLI(connected)
	if target == "" {
		target = s.activeHTTPProvider(ctx)
	}
	if target == "" {
		return 0, nil
	}
	isConnected := make(map[domain.LLMProviderType]bool, len(connected))
	for _, p := range connected {
		isConnected[p] = true
	}

	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	model, heavy := domain.ProviderDefaultModels(target)
	moved := 0
	for _, agent := range agents {
		adopt := agent.ProviderType == "" && agent.CatalogSlug != ""
		stranded := domain.RequiresHostExecutor(agent.ProviderType) && !isConnected[agent.ProviderType]
		if !adopt && !stranded {
			continue
		}
		agent.ProviderType = target
		agent.Model, agent.ModelHeavy = model, heavy
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

// Only a configured HTTP provider counts; moves onto an unconfigured default endpoint would strand the agent.
func (s *Service) activeHTTPProvider(ctx context.Context) domain.LLMProviderType {
	if s.providers == nil {
		return ""
	}
	active, err := s.providers.GetActiveProvider(ctx)
	if err != nil || active == "" || domain.RequiresHostExecutor(active) {
		return ""
	}
	cfg, err := s.providers.Get(ctx, active)
	if err != nil || !cfg.Configured {
		return ""
	}
	return active
}

func (s *Service) SetLLMProviders(store LLMProviders) {
	s.providers = store
}
