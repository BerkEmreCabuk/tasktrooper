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

// The replanner prompt defines neither "ready" nor "questions" — a repair plan
// never interrogates the user. Judging it by the planner's rules rejected every
// replan on the first field it looked at, so repair tasks never reached the
// executor.
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

// The planner itself keeps the strict rule: a plan with no "questions" key is
// off-schema output worth retrying.
func TestParsePlannerOutput_StillRequiresQuestions(t *testing.T) {
	_, err := orchestrator.ParsePlannerOutputForTest(`{"ready":true,"summary":"plan","tasks":[]}`)
	assert.ErrorContains(t, err, "questions")
}

// Repair plans go through the same disjoint-write validator as first plans, but
// the replanner prompt never mentioned parallel_group at all — so it could only
// satisfy the rule by luck.
func TestBuildReplannerSystemPrompt_StatesBoardWriteConstraint(t *testing.T) {
	p := orchestrator.BuildReplannerSystemPromptForTest()

	assert.Contains(t, p, "AT MOST ONE task may list a board-write tool")
	assert.Contains(t, p, "cannot see each other's work")
	assert.Contains(t, p, "never by creating a second one for the same work")
}
