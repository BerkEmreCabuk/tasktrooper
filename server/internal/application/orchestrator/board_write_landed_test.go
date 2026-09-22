package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/stretchr/testify/assert"
)

// The tracker counts successes only, so all-rejected moves leave the same empty ledger as no calls at all.
func TestBoardWriteNotLanded_ReportsABookkeepingSubtaskThatNeverMoved(t *testing.T) {
	reason := orchestrator.BoardWriteNotLandedReasonForTest(nil, []string{"move_board_task"})

	assert.Contains(t, reason, "no such call succeeded")
}

func TestBoardWriteNotLanded_SilentWhenTheMoveLanded(t *testing.T) {
	reason := orchestrator.BoardWriteNotLandedReasonForTest(
		map[string]int{"move_board_task": 1}, []string{"move_board_task"})

	assert.Empty(t, reason)
}

// An implementer is finished by its code, not its column: it may list the move and never make it.
func TestBoardWriteNotLanded_IgnoresSubtasksWithRealWork(t *testing.T) {
	reason := orchestrator.BoardWriteNotLandedReasonForTest(
		map[string]int{"run_terminal": 3}, []string{"run_terminal", "move_board_task"})

	assert.Empty(t, reason)
}

// A conversational subtask calls nothing and declares no board write.
func TestBoardWriteNotLanded_IgnoresSubtasksWithoutABoardWrite(t *testing.T) {
	reason := orchestrator.BoardWriteNotLandedReasonForTest(nil, []string{"add_task_comment"})

	assert.Empty(t, reason)
}
