package agent

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const groundClarificationNote = "ask_user rejected: this run has not read the repository yet, " +
	"so it cannot know which of its questions the code already answers. " +
	"Look first — codebase_search / grep_code / get_repo_tree / get_symbol_skeleton / expand_symbol_context " +
	"work in the task workspace and answer anything about file layout, existing components, routing or configuration. " +
	"Ask the human only about what the repository cannot contain: product decisions, priorities, external URLs, " +
	"credentials, or which of several valid designs they want. Then call ask_user again if something is still unknown."

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
