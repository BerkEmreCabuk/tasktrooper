package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// implementationTools is the shape a subtask asked to change code should have.
var implementationTools = []string{"claim_board_task", "move_board_task", "run_terminal", "grep_code"}

func TestStartedNotFinished_ClaimAndMoveOnlyIsNotDone(t *testing.T) {
	// The card read completed while the result admitted it never started the work.
	reason := orchestrator.StartedNotFinishedReasonForTest(
		map[string]int{"claim_board_task": 1, "move_board_task": 1},
		implementationTools,
	)

	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "does not complete the subtask")
}

func TestStartedNotFinished_RealWorkPasses(t *testing.T) {
	reason := orchestrator.StartedNotFinishedReasonForTest(
		map[string]int{"claim_board_task": 1, "move_board_task": 2, "run_terminal": 6},
		implementationTools,
	)

	assert.Empty(t, reason, "one non-bookkeeping call is enough to show the subtask did something")
}

func TestStartedNotFinished_MoveOnlySubtaskIsComplete(t *testing.T) {
	// The move IS the deliverable; a run whose whole ledger is that move finished its job.
	reason := orchestrator.StartedNotFinishedReasonForTest(
		map[string]int{"move_board_task": 1},
		[]string{"move_board_task", "ask_user"},
	)

	assert.Empty(t, reason)
}

func TestStartedNotFinished_NoToolCallsPasses(t *testing.T) {
	// A subtask that answers from context calls nothing; failing it would break every conversational plan.
	reason := orchestrator.StartedNotFinishedReasonForTest(nil, implementationTools)

	assert.Empty(t, reason)
}

func TestUsageDelta_IsolatesOneSubtaskFromASharedTracker(t *testing.T) {
	// Board runs share one tracker across subtasks; the per-subtask check reads a difference, not a total.
	_, usage := registry.ContextWithToolUsage(t.Context())
	usage.Record("grep_code")
	before := usage.Snapshot()

	usage.Record("claim_board_task")
	usage.Record("move_board_task")
	usage.Record("move_board_task")

	delta := registry.UsageDelta(before, usage.Snapshot())

	assert.Equal(t, map[string]int{"claim_board_task": 1, "move_board_task": 2}, delta,
		"the earlier subtask's grep_code must not count as this subtask's work")
}
