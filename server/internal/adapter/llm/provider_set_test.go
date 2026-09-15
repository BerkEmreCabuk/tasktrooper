package llm

import (
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// staticSet is the ProviderSet shape every test in this package resolves
// through: one default provider and whatever clients the case needs.
func staticSet(def domain.LLMProviderType, clients map[domain.LLMProviderType]port.LLMClient) ProviderSet {
	return ProviderSet{Clients: clients, Default: def}
}
