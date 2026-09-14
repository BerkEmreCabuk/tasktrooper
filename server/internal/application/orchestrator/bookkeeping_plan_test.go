package orchestrator_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func implementTask(agentID uuid.UUID, id, title string) domain.PlannerTask {
	return domain.PlannerTask{
		ID: id, Title: title, Description: "implement it",
		AgentID: agentID.String(), ToolNames: []string{"run_terminal"},
	}
}

// DE-1's plan ended with a step whose entire content was one move_board_task
// call. It cost a model call, its own retries and its own card, nothing verified
// it, and the board recorded no move — twice, because the repair plan copied it.
func TestValidatePlannerOutput_RejectsAMoveOnlySubtask(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "frontend-developer", Enabled: true}}
	plan := domain.PlannerOutput{
		Ready: true, Summary: "s", Questions: []domain.ClarificationQuestion{},
		Tasks: []domain.PlannerTask{
			implementTask(agentID, "t1", "Implement Android button and link"),
			{
				ID: "t2", Title: "Move task to code_review", Description: "hand it off",
				AgentID: agentID.String(), ToolNames: []string{"move_board_task"},
				DependsOn: []string{"t1"}, ParallelGroup: 1,
			},
		},
	}

	err := orchestrator.ValidatePlannerOutputForTest(plan, agents, nil, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "board bookkeeping")
}

// The single-subtask case is a real request ("move DE-1 to done"), and the ban
// must not swallow it.
func TestValidatePlannerOutput_AllowsABookkeepingOnlyPlanOfOneTask(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "product-manager", Enabled: true}}
	plan := domain.PlannerOutput{
		Ready: true, Summary: "s", Questions: []domain.ClarificationQuestion{},
		Tasks: []domain.PlannerTask{{
			ID: "t1", Title: "Move DE-1 to done", Description: "the stakeholder asked for it",
			AgentID: agentID.String(), ToolNames: []string{"move_board_task"},
		}},
	}

	err := orchestrator.ValidatePlannerOutputForTest(plan, agents, nil, 10)

	assert.NoError(t, err, "a plan whose whole deliverable is the move is legitimate")
}

// A comment is not a column change: hand-off notes and stakeholder answers stay
// plannable.
func TestValidatePlannerOutput_AllowsACommentOnlySubtask(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "product-manager", Enabled: true}}
	plan := domain.PlannerOutput{
		Ready: true, Summary: "s", Questions: []domain.ClarificationQuestion{},
		Tasks: []domain.PlannerTask{
			implementTask(agentID, "t1", "Write the release note"),
			{
				ID: "t2", Title: "Tell the stakeholder", Description: "post the summary",
				AgentID: agentID.String(), ToolNames: []string{"add_task_comment"},
				DependsOn: []string{"t1"}, ParallelGroup: 1,
			},
		},
	}

	err := orchestrator.ValidatePlannerOutputForTest(plan, agents, nil, 10)

	assert.NoError(t, err)
}

// The repair plan that produced DE-1's duplicate cards: same titles as the
// subtasks that had already run, so the board showed each step twice — one copy
// completed, one still working.
func TestValidateRepairPlan_RejectsATitleTheRunAlreadyRan(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "frontend-developer", Enabled: true}}
	prior := []domain.PlannerTask{implementTask(agentID, "t1", "Implement Android button and link")}
	repair := domain.PlannerOutput{
		Ready: true, Summary: "repair", Questions: []domain.ClarificationQuestion{},
		Tasks: []domain.PlannerTask{
			implementTask(agentID, "r1", "  implement android BUTTON and link "),
		},
	}

	err := orchestrator.ValidateRepairPlanForTest(repair, agents, nil, 10, prior)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeats the title")
}

// Repair with its own scope is exactly what the replanner is for.
func TestValidateRepairPlan_AcceptsADistinctRepairTitle(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "frontend-developer", Enabled: true}}
	prior := []domain.PlannerTask{implementTask(agentID, "t1", "Implement Android button and link")}
	repair := domain.PlannerOutput{
		Ready: true, Summary: "repair", Questions: []domain.ClarificationQuestion{},
		Tasks: []domain.PlannerTask{
			implementTask(agentID, "r1", "Add the missing Android store URL to the config"),
		},
	}

	err := orchestrator.ValidateRepairPlanForTest(repair, agents, nil, 10, prior)

	assert.NoError(t, err)
}
