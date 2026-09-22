package domain

import "errors"

// Lifecycle-gate errors. The board's two terminal columns are claims about the
// world, and until these gates existed nothing checked either one: done claims
// "this passed its review chain" (a card could be dragged straight into done
// without any reviewer ever seeing it), and released claims "this is live in
// production" (a task could be marked shipped while its code sat on an unmerged
// branch). Both fail closed on unreadable evidence, for the same reason
// releaseTargetGate does: a check that passes when its input is missing is not
// a check.
var (
	// ErrReviewChainIncomplete blocks done/released for a task that never
	// passed one of the stages its type requires; the wrapped detail names the
	// missing stages and the move that earns each one.
	ErrReviewChainIncomplete = errors.New("done means the task passed its review chain, and this one has not")
	// ErrReviewStageRejected blocks done/released for a task whose most recent
	// visit to a review stage ended in a recorded rejection — having visited a
	// gate is not the same as having passed it.
	ErrReviewStageRejected = errors.New("a review stage rejected this task and it has not been re-reviewed since")
	// ErrReleaseNotDeployed blocks released for a task with no successful
	// production deploy recorded against it.
	ErrReleaseNotDeployed = errors.New("released means the task is live in production, and no successful production deploy is recorded for it")
)

// ReviewStage is one mandatory step of a task's review chain.
//
// Column is the evidence: task_column_spans records every visit a task makes to
// every column, so "has this task ever been in code_review" answers "was it
// reviewed" without depending on who moved it or on a verdict field that is
// only written when require_human_review is on. A task that bounced through
// need_revision and came back keeps its earlier spans, so rework is not
// punished.
type ReviewStage struct {
	// Column must appear in the task's span history for the stage to count.
	Column TaskColumn
	// Label is what a block message calls this stage to a human.
	Label string
	// Remedy is the move that earns the stage, named in the block message so
	// the error is actionable rather than merely correct.
	Remedy string
}

// ReviewChainForType and TaskTypeShipsCode are gone: a type's review chain is
// now Workflow.ReviewChain() (built from each stage's review_chain_stage
// behaviour) and "ships code" is whether the released stage carries
// require_release_deploy — see application/repository.Service.reviewChainGate
// and releaseDeployGate.
