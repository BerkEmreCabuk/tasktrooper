package repository

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// TestGolden_RepositoryGuardWordingPart3 pins the exact byte output of the
// work-order/deploy-order cycle refusals (workorder.go), the test-case
// validation refusals (test_cases.go), and the review-chain gate refusals
// (lifecyclegate.go): every one reaches an agent as a tool-call error —
// set_blockers/create_task/update_task, save_test_cases, and
// move_board_task/merge_task_pull_request respectively.
func TestGolden_RepositoryGuardWordingPart3(t *testing.T) {
	assert := func(got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	assert(workOrderCycleKey.Render(workOrderCycleInput{Task: "T-1", Blocker: "T-2", Path: "T-1 → T-2"}),
		"work-order cycle refused: T-1 already has to be finished before T-2 (T-1 → T-2), so it cannot also wait for it")

	assert(deployOrderCycleKey.Render(deployOrderCycleInput{Dependency: "T-1", Task: "T-2", Path: "T-1 → T-2"}),
		"deploy-order cycle refused: T-1 already ships after T-2 (T-1 → T-2), so it cannot also ship before it")

	assert(testCaseDuplicateTitleKey.Render(testCaseDuplicateTitleInput{Title: `"logs in"`}),
		`two test cases share the title "logs in"; titles identify a case, so give them distinct ones`)

	assert(testCaseCriterionNotOnTaskKey.Render(testCaseCriterionNotOnTaskInput{CriterionID: "11111111-1111-1111-1111-111111111111"}),
		"criterion 11111111-1111-1111-1111-111111111111 is not on this task; leave criterion_id empty for a case no criterion states")

	assert(reviewChainWorkflowUnreadableKey.Render(reviewChainErrInput{Err: "no workflow configured"}),
		"its workflow could not be read (no workflow configured) — retry the move")
	assert(reviewChainHistoryUnreadableKey.Render(reviewChainErrInput{Err: "db down"}),
		"its stage history could not be read (db down) — retry the move")
	assert(prompt.Text(reviewChainNoSpanStoreKey),
		"the column-span ledger is not available, so its review history cannot be read. Fix the control plane's span store")
	assert(reviewChainStageRejectedKey.Render(reviewChainStageRejectedInput{Task: "T-1", Target: "done", Rejected: "QA (in_qa) — x"}),
		"cannot move T-1 to done. Rejected at: QA (in_qa) — x")
	assert(reviewChainMissingStagesKey.Render(reviewChainMissingStagesInput{Task: "T-1", Target: "done", Missing: "QA (in_qa) — x"}),
		"cannot move T-1 to done. Missing: QA (in_qa) — x")
}
