package board

import (
	"context"

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
//
// application/board.ungroundedAnalysisReason (guard.ungrounded_analysis) says
// the same underlying thing at the run-gate that gets the LAST word on a
// finished analiz task, in one flat sentence naming a fixed tool list. This
// one is the FIRST word, at the point of the write itself, and stays a
// separate catalog entry (guard.board_ungrounded_analysis_grounding) on
// purpose: it names domain.CodeExplorationTools live rather than a wording
// copy of it, and it goes on to tell the agent HOW to fix it (which tool for
// which kind of evidence) rather than just stating that it must. Collapsing
// the two would mean picking one voice for two different moments, or
// re-deriving the tool list from the run-gate's static prose.
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
	return ungroundedAnalysisGroundingKey.Render(ungroundedAnalysisGroundingInput{Tools: domain.CodeExplorationTools})
}
