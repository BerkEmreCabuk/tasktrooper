package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReplannerOutput_RequiresToolNames(t *testing.T) {
	agentID := "550e8400-e29b-41d4-a716-446655440000"
	raw := `{"summary":"repair","tasks":[{"id":"r1","title":"Fix","description":"Add tests","agent_id":"` + agentID + `","skill_ids":[],"subtask_rules":[],"depends_on":["t1"],"parallel_group":0}]}`
	_, err := orchestrator.ParseRepairPlanOutputForTest(raw)
	assert.ErrorContains(t, err, "tool_names")
}

// The replanner's output shape never sets "ready" or "questions"; judging it by the planner's rules rejected every replan.
func TestParseReplannerOutput_AcceptsPromptShapeWithoutReadyOrQuestions(t *testing.T) {
	agentID := "550e8400-e29b-41d4-a716-446655440000"
	raw := `{"summary":"repair","tasks":[{"id":"r1","title":"Fix","description":"Add tests","agent_id":"` + agentID + `","skill_ids":[],"tool_names":["run_terminal"],"subtask_rules":[],"depends_on":["t1"],"parallel_group":0}]}`

	output, err := orchestrator.ParseRepairPlanOutputForTest(raw)
	require.NoError(t, err)
	assert.True(t, output.Ready)
	require.Len(t, output.Tasks, 1)
	assert.Equal(t, "r1", output.Tasks[0].ID)
	assert.Equal(t, []string{"run_terminal"}, output.Tasks[0].ToolNames)
}

// The planner itself keeps the strict rule: a plan without "questions" is worth retrying.
func TestParsePlannerOutput_StillRequiresQuestions(t *testing.T) {
	_, err := orchestrator.ParsePlannerOutputForTest(`{"ready":true,"summary":"plan","tasks":[]}`)
	assert.ErrorContains(t, err, "questions")
}

// Repair plans share the disjoint-write validator, so the replanner prompt has to state the rule itself.
func TestBuildReplannerSystemPrompt_StatesBoardWriteConstraint(t *testing.T) {
	p := orchestrator.BuildReplannerSystemPromptForTest()

	assert.Contains(t, p, "AT MOST ONE task may list a board-write tool")
	assert.Contains(t, p, "cannot see each other's work")
	assert.Contains(t, p, "never by creating a second one for the same work")
}
