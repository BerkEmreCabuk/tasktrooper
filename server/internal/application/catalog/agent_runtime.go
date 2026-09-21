package catalog

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// LLMProviders is the slice of the provider store the reconciler reads: which
// provider is active, and whether it is configured.
type LLMProviders interface {
	GetActiveProvider(ctx context.Context) (domain.LLMProviderType, error)
	Get(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProviderConfig, error)
}

// cliPreference orders the agent CLIs when several are connected. Any CLI beats
// an HTTP fallback; among CLIs, declaration order breaks a tie, so the first
// host-executed provider in the registry is the default. A new CLI joins the
// race by being declared — nothing here has to name it.
var cliPreference = func() []domain.LLMProviderType {
	var out []domain.LLMProviderType
	for _, def := range domain.AllLLMProviderDefinitions() {
		if def.HostExecuted {
			out = append(out, def.Type)
		}
	}
	return out
}()

// ReconcileAgentRuntimes points agents at a provider that can run them after the
// set of available providers changes, and returns how many it moved.
//
// The target is the best connected CLI, or — when no CLI is connected — the
// install's active HTTP provider if it is configured. An agent on a CLI that is
// not connected moves to the target, and a catalog agent with no provider
// adopts it. Agents on a configured HTTP provider, and custom agents left on
// the default provider, are someone's choice and stay put. With nothing usable
// available nothing moves: an agent still on a disconnected CLI fails with
// "connect it", which says what to do, where the default provider would fail
// with something vaguer.
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

// activeHTTPProvider is the install's active provider when it is an HTTP one
// that has actually been configured, so moves onto it cannot land an agent on a
// default endpoint nobody set up. A host-executed active provider is handled by
// the CLI branch instead.
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

// SetLLMProviders lets the reconciler fall back to the install's active HTTP
// provider. Optional: without it, only connected CLIs are considered.
func (s *Service) SetLLMProviders(store LLMProviders) {
	s.providers = store
}
