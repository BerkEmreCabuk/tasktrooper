package agent

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var groundClarificationNote = prompt.Text(clarificationRefusalKey)

type clarificationGate struct {
	explorationAvailable bool
	refused              bool
}

var groundingTools = append(append([]string{}, domain.CodeExplorationTools...), "run_terminal")

func newClarificationGate(tools []domain.ToolDefinition) *clarificationGate {
	available := make(map[string]bool, len(tools))
	for _, t := range tools {
		available[t.Function.Name] = true
	}
	for _, name := range groundingTools {
		if available[name] {
			return &clarificationGate{explorationAvailable: true}
		}
	}
	return &clarificationGate{}
}

func (g *clarificationGate) refuse(ctx context.Context) bool {
	if g == nil || g.refused || !g.explorationAvailable {
		return false
	}
	usage := registry.ToolUsageFromContext(ctx)
	if usage == nil || usage.UsedAny(groundingTools...) {
		return false
	}
	g.refused = true
	return true
}
