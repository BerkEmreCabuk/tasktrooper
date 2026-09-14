package orchestrator

import (
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDisjointWrites_RejectsTwoWritersInOneGroup(t *testing.T) {
	// The shape that produced two board tasks for one request.
	err := validateDisjointWrites([]domain.PlannerTask{
		{ID: "t1", ToolNames: []string{"create_board_task"}, ParallelGroup: 0},
		{ID: "t2", ToolNames: []string{"create_board_task", "move_board_task"}, ParallelGroup: 0},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parallel_group 0")
	assert.Contains(t, err.Error(), "depends_on")
}

func TestValidateDisjointWrites_AllowsWritersInDifferentGroups(t *testing.T) {
	err := validateDisjointWrites([]domain.PlannerTask{
		{ID: "t1", ToolNames: []string{"create_board_task"}, ParallelGroup: 0},
		{ID: "t2", ToolNames: []string{"move_board_task"}, ParallelGroup: 1, DependsOn: []string{"t1"}},
	})

	assert.NoError(t, err)
}

func TestValidateDisjointWrites_AllowsParallelReaders(t *testing.T) {
	err := validateDisjointWrites([]domain.PlannerTask{
		{ID: "t1", ToolNames: []string{"list_board_tasks"}, ParallelGroup: 0},
		{ID: "t2", ToolNames: []string{"web_search", "list_repositories"}, ParallelGroup: 0},
		{ID: "t3", ToolNames: []string{"create_board_task"}, ParallelGroup: 0},
	})

	assert.NoError(t, err, "one writer beside readers is fine")
}

func TestValidateDisjointWrites_CountsEachSubtaskOnce(t *testing.T) {
	// A single subtask holding several write tools is not a conflict.
	err := validateDisjointWrites([]domain.PlannerTask{
		{ID: "t1", ToolNames: []string{"create_board_task", "move_board_task", "update_board_task"}, ParallelGroup: 0},
	})

	assert.NoError(t, err)
}

func TestValidateDisjointWrites_UndeclaredToolsAreInvisible(t *testing.T) {
	// Documents this check's limit: a subtask that declares no tool_names
	// inherits its agent's whole policy, and the per-group check reads
	// declarations only. validateSingleTaskCreator resolves the agent policy
	// instead, which is what covers the undeclared case.
	err := validateDisjointWrites([]domain.PlannerTask{
		{ID: "t1", ParallelGroup: 0},
		{ID: "t2", ParallelGroup: 0},
	})

	assert.NoError(t, err)
}

func TestValidateSingleTaskCreator_UndeclaredToolsResolveAgainstAgentPolicy(t *testing.T) {
	// The hole the per-group check leaves open: two subtasks that declare
	// nothing, assigned to an agent that may open board tasks. Neither is
	// visible as a writer, and both open one.
	pm := uuid.New().String()
	policies := map[string]domain.ToolPolicy{
		pm: {AllowTools: []string{"create_board_task", "move_board_task", "list_board_tasks"}},
	}

	err := validateSingleTaskCreator([]domain.PlannerTask{
		{ID: "t1", AgentID: pm},
		{ID: "t2", AgentID: pm},
	}, policies)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tool_names inherits its agent's whole toolset",
		"the message has to name the remedy: declare tool_names")
}

func TestValidateSingleTaskCreator_UndeclaredIsFineForAgentsThatCannotCreate(t *testing.T) {
	// Only product-manager is seeded with create_board_task, so developer and QA
	// subtasks must not be dragged into this rule just for omitting tool_names.
	dev := uuid.New().String()
	policies := map[string]domain.ToolPolicy{
		dev: {AllowTools: []string{"claim_board_task", "move_board_task", "run_terminal", "grep_code"}},
	}

	err := validateSingleTaskCreator([]domain.PlannerTask{
		{ID: "t1", AgentID: dev},
		{ID: "t2", AgentID: dev},
	}, policies)

	assert.NoError(t, err)
}

func TestBuildPlannerSystemPrompt_StatesTheDisjointRule(t *testing.T) {
	p := BuildPlannerSystemPromptWithOptionsForTest(PlannerOptions{Lang: "tr"})

	assert.Contains(t, p, "disjoint deliverables")
	assert.Contains(t, p, "depends_on rather than the same parallel_group")
}

func TestValidateSingleTaskCreator_RejectsCreatorsChainedAcrossGroups(t *testing.T) {
	// The plan that produced DE-1/DE-2/DE-3 for one request: a "decide where it
	// goes" step, a "decide how it looks" step and the real work, each in its own
	// group so validateDisjointWrites had no objection, each opening a task.
	tasks := []domain.PlannerTask{
		{ID: "t1", ToolNames: []string{"create_board_task"}, ParallelGroup: 0},
		{ID: "t2", ToolNames: []string{"create_board_task"}, ParallelGroup: 1, DependsOn: []string{"t1"}},
		{ID: "t3", ToolNames: []string{"create_board_task"}, ParallelGroup: 2, DependsOn: []string{"t2"}},
	}
	require.NoError(t, validateDisjointWrites(tasks), "groups are disjoint; only the run-wide rule catches this")

	err := validateSingleTaskCreator(tasks, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "t1, t2, t3")
	assert.Contains(t, err.Error(), "at most one subtask")
}

func TestValidateSingleTaskCreator_AllowsOneCreatorBesideMovers(t *testing.T) {
	err := validateSingleTaskCreator([]domain.PlannerTask{
		{ID: "t1", ToolNames: []string{"create_board_task"}, ParallelGroup: 0},
		{ID: "t2", ToolNames: []string{"move_board_task", "add_task_comment"}, ParallelGroup: 1, DependsOn: []string{"t1"}},
		{ID: "t3", ToolNames: []string{"update_board_task"}, ParallelGroup: 2, DependsOn: []string{"t2"}},
	}, nil)

	assert.NoError(t, err, "acting on the record the run opened is the wanted shape")
}

func TestValidatePlannerOutput_CountsCreatorsAcrossRepairPlans(t *testing.T) {
	// The other route to a duplicate: the plan opened the right task, the
	// verifier scored the run failed because the feature was not live yet, and
	// the repair plan opened a second "technical analysis" task for it.
	agentID := uuid.New()
	prior := []domain.PlannerTask{
		{ID: "t1", Title: "Open the task", Description: "d", AgentID: agentID.String(), ToolNames: []string{"create_board_task"}},
	}
	repair := domain.PlannerOutput{
		Ready:     true,
		Summary:   "repair",
		Questions: []domain.ClarificationQuestion{},
		Tasks: []domain.PlannerTask{
			{ID: "r1", Title: "Open an analiz task", Description: "d", AgentID: agentID.String(), ToolNames: []string{"create_board_task"}},
		},
	}
	agents := []domain.Agent{{ID: agentID, Name: "product-manager", Enabled: true}}

	require.NoError(t, validatePlannerOutput(repair, agents, nil, 10, nil),
		"the repair plan is fine on its own — only the run-wide count rejects it")

	err := validatePlannerOutput(repair, agents, nil, 10, prior)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "r1")
	assert.Contains(t, err.Error(), "t1")
}

func TestBuildPlannerSystemPrompt_ForbidsLookupAndDecisionSubtasks(t *testing.T) {
	p := BuildPlannerSystemPromptWithOptionsForTest(PlannerOptions{Lang: "tr"})

	assert.Contains(t, p, "A subtask must produce a CHANGE, not a decision or a fact")
	assert.Contains(t, p, "the implementing subtask looks it up itself with its own tools")
	assert.Contains(t, p, "AT MOST ONE subtask in the whole plan may create board tasks")
	assert.Contains(t, p, "A subtask with an EMPTY tool_names inherits its assigned agent's entire toolset")
	assert.Contains(t, p, "tool_names governs BOARD WRITES only",
		"a short tool list must not read as a capability budget: that is what blinded an implementer")
	assert.Contains(t, p, "can never leave an implementer unable to look at the repository")
	assert.Contains(t, p, "move_board_task / update_board_task / add_task_comment",
		"a request about an existing record is an action on that record")
}

func TestBuildReplannerSystemPrompt_ForbidsRepairingBoardLatency(t *testing.T) {
	p := BuildReplannerSystemPromptForTest()

	assert.Contains(t, p, "create_board_task is counted across the original plan AND this repair plan")
	assert.Contains(t, p, "the feature is not live yet")
}
