package orchestrator_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlannerOutput_Valid(t *testing.T) {
	agentID := uuid.New().String()
	raw := `{"ready":true,"summary":"Do things","questions":[],"tasks":[{"id":"t1","title":"First","description":"Do first","agent_id":"` + agentID + `","skill_ids":[],"tool_names":[],"subtask_rules":[],"depends_on":[],"parallel_group":0}]}`
	output, err := orchestrator.ParsePlannerOutputForTest(raw)
	require.NoError(t, err)
	assert.Equal(t, "Do things", output.Summary)
	require.Len(t, output.Tasks, 1)
	assert.Equal(t, "t1", output.Tasks[0].ID)
}

func TestParsePlannerOutput_InvalidJSON(t *testing.T) {
	_, err := orchestrator.ParsePlannerOutputForTest(`not json`)
	assert.Error(t, err)
}

func TestParsePlannerOutput_MissingToolNames(t *testing.T) {
	agentID := uuid.New().String()
	raw := `{"ready":true,"summary":"Do things","questions":[],"tasks":[{"id":"t1","title":"First","description":"Do first","agent_id":"` + agentID + `","skill_ids":[],"subtask_rules":[],"depends_on":[],"parallel_group":0}]}`
	_, err := orchestrator.ParsePlannerOutputForTest(raw)
	assert.Error(t, err)
}

func TestValidatePlannerOutput_SkillOwnership(t *testing.T) {
	agentID := uuid.New()
	otherAgentID := uuid.New()
	skillID := uuid.New().String()
	output := domain.PlannerOutput{
		Ready:   true,
		Summary: "ok",
		Tasks: []domain.PlannerTask{{
			ID: "t1", Title: "T", Description: "D",
			AgentID: agentID.String(), SkillIDs: []string{skillID}, ToolNames: []string{},
		}},
	}
	agents := []domain.Agent{{ID: agentID, Enabled: true}, {ID: otherAgentID, Enabled: true}}
	ownership := map[string]map[string]bool{
		agentID.String():      {skillID: true},
		otherAgentID.String(): {},
	}
	err := orchestrator.ValidatePlannerOutputForTest(output, agents, ownership, 10)
	assert.NoError(t, err)

	output.Tasks[0].AgentID = otherAgentID.String()
	err = orchestrator.ValidatePlannerOutputForTest(output, agents, ownership, 10)
	assert.Error(t, err)
}

func TestValidatePlannerOutput_MissingSummary(t *testing.T) {
	agentID := uuid.New().String()
	err := orchestrator.ValidatePlannerOutputForTest(domain.PlannerOutput{
		Summary: "",
		Tasks:   []domain.PlannerTask{{ID: "t1", Title: "T", Description: "D", AgentID: agentID, ToolNames: []string{}}},
	}, []domain.Agent{{ID: uuid.MustParse(agentID), Enabled: true}}, nil, 10)
	assert.Error(t, err)
}

func TestTopologicalWaves_Linear(t *testing.T) {
	waves, err := orchestrator.TopologicalWaves([]domain.PlannerTask{
		{ID: "a", DependsOn: nil},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	})
	require.NoError(t, err)
	require.Len(t, waves, 3)
	assert.Equal(t, []string{"a"}, waves[0])
	assert.Equal(t, []string{"b"}, waves[1])
	assert.Equal(t, []string{"c"}, waves[2])
}

func TestTopologicalWaves_Parallel(t *testing.T) {
	waves, err := orchestrator.TopologicalWaves([]domain.PlannerTask{
		{ID: "a", DependsOn: nil},
		{ID: "b", DependsOn: nil},
		{ID: "c", DependsOn: []string{"a", "b"}},
	})
	require.NoError(t, err)
	require.Len(t, waves, 2)
	assert.ElementsMatch(t, []string{"a", "b"}, waves[0])
	assert.Equal(t, []string{"c"}, waves[1])
}

func TestTopologicalWaves_Cycle(t *testing.T) {
	_, err := orchestrator.TopologicalWaves([]domain.PlannerTask{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	})
	assert.Error(t, err)
}

func TestShouldOrchestrate_Force(t *testing.T) {
	assert.True(t, orchestrator.ShouldOrchestrate(true, true))
}

func TestShouldOrchestrate_FastPathOff(t *testing.T) {
	assert.False(t, orchestrator.ShouldOrchestrate(false, true))
}

func TestShouldOrchestrate_FastPathDisabled(t *testing.T) {
	assert.True(t, orchestrator.ShouldOrchestrate(false, false))
}

// The planner prompt lists skills by metadata only; the executor loads the bodies on demand.
func TestBuildPlannerSystemPrompt_SkillsMetadataOnly(t *testing.T) {
	skills := []domain.Skill{
		{ID: uuid.New(), Name: "go-test", Description: "Testing in Go", Category: "testing", Content: "use testify suites for table tests", Enabled: true},
	}
	prompt := orchestrator.BuildPlannerSystemPromptLegacyForTest(nil, skills, nil)
	assert.Contains(t, prompt, "go-test")
	assert.Contains(t, prompt, "Testing in Go")
	assert.NotContains(t, prompt, "use testify suites for table tests")
}

func TestIsolatedSubtaskHistory_KeepsConversationDropsToolChatter(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "original"},
		{Role: domain.RoleAssistant, Content: "summary", ToolCalls: nil},
		{Role: domain.RoleUser, Content: "follow up"},
		{Role: domain.RoleTool, Content: "tool output"},
	}
	isolated := orchestrator.IsolatedSubtaskHistoryForTest(history)
	require.Len(t, isolated, 3)
	assert.Equal(t, "original", isolated[0].Content)
	assert.Equal(t, "summary", isolated[1].Content)
	assert.Equal(t, "follow up", isolated[2].Content)
}

func TestTruncateDependencyOutput(t *testing.T) {
	long := strings.Repeat("x", 100)
	out := orchestrator.TruncateDependencyOutputForTest(long, 50)
	assert.True(t, len(out) > 50)
	assert.Contains(t, out, "[truncated")
}
