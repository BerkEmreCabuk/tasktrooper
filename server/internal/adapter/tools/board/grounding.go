package board

import (
	"context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ungroundedAnalysisReason returns a message to hand back to the agent when it
// tries to publish an analiz result without having read the repository in this
// run, or "" when the output is grounded (or the run is not measured).
//
// The failure it prevents: a system-architect run produced a complete technical
// spec for a repository it never opened — its whole tool ledger was one
// load_skill and seven `echo "<spec>" >` shell calls. Prose instructions to
// explore first already existed in the agent's skill; a weak model ignored
// them. This check is not advice, so it cannot be ignored.
func ungroundedAnalysisReason(ctx context.Context) string {
	usage := registry.ToolUsageFromContext(ctx)
	if usage == nil {
		// Nobody is measuring this run (chat sessions, tests). An unmeasured run
		// is not a failed one — never block on missing evidence.
		return ""
	}
	if usage.UsedAny(domain.CodeExplorationTools...) {
		return ""
	}
	return "An analiz result must be based on the repository, and this run has not read it yet: " +
		"no " + strings.Join(domain.CodeExplorationTools, ", ") + " call has succeeded. " +
		"Explore the code first (get_repo_tree for structure, codebase_search for concepts, " +
		"grep_code for exact symbols, expand_symbol_context to read the parts that matter), " +
		"then write the analysis naming the real files and interfaces you found."
}
