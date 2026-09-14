package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedLLM replays one canned response per call and records the messages
// (and the full request, for callers that need more than that) it was handed,
// so a test can assert what the retry actually sent back.
type scriptedLLM struct {
	responses []string
	calls     [][]domain.Message
	requests  []domain.AgentRequest
}

func (s *scriptedLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	s.calls = append(s.calls, req.Messages)
	s.requests = append(s.requests, req)
	content := s.responses[len(s.calls)-1]
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: content}}, nil
}

func (s *scriptedLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (s *scriptedLLM) Models(_ context.Context) ([]string, error) { return nil, nil }
func (s *scriptedLLM) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, nil
}

// singleAgentCatalog answers only the three lookups the planner performs; the
// embedded interface leaves the rest unimplemented on purpose.
type singleAgentCatalog struct {
	port.CatalogStore
	agent domain.Agent
}

func (c singleAgentCatalog) ListAgents(_ context.Context) ([]domain.Agent, error) {
	return []domain.Agent{c.agent}, nil
}

func (c singleAgentCatalog) ListSkillsByAgent(_ context.Context, _ uuid.UUID) ([]domain.Skill, error) {
	return nil, nil
}

func (c singleAgentCatalog) ListEnabledRulesByAgent(_ context.Context, _ uuid.UUID) ([]domain.OrchestratorRule, error) {
	return nil, nil
}

// A validation failure used to leave the messages untouched, so all three
// attempts sent the identical prompt, the model returned the identical rejected
// plan, and a fixable plan reached the user as "planner failed after 3
// attempts". The rejection has to go back to the model.
func TestPlannerGenerate_FeedsValidationErrorBackToTheModel(t *testing.T) {
	agentID := uuid.New()
	rejected := `{"ready":true,"summary":"plan","questions":[],"tasks":[
		{"id":"t1","title":"Create","description":"Open the task","agent_id":"` + agentID.String() + `","skill_ids":[],"tool_names":["create_board_task"],"subtask_rules":[],"depends_on":[],"parallel_group":0},
		{"id":"t2","title":"Move","description":"Move the task","agent_id":"` + agentID.String() + `","skill_ids":[],"tool_names":["move_board_task"],"subtask_rules":[],"depends_on":[],"parallel_group":0}]}`
	accepted := `{"ready":true,"summary":"plan","questions":[],"tasks":[
		{"id":"t1","title":"Create","description":"Open the task","agent_id":"` + agentID.String() + `","skill_ids":[],"tool_names":["create_board_task"],"subtask_rules":[],"depends_on":[],"parallel_group":0}]}`

	llm := &scriptedLLM{responses: []string{rejected, accepted}}
	catalog := singleAgentCatalog{agent: domain.Agent{ID: agentID, Name: "product-manager", Enabled: true}}
	planner := orchestrator.NewPlanner(llm, catalog, nil, 10, 5)

	output, err := planner.Generate(
		context.Background(),
		domain.GoalIntake{Ready: true, Purpose: "p", Goal: "g"},
		"siteye android linki ekle",
		nil, "test-model", nil,
		orchestrator.PlannerOptions{Lang: "tr"},
	)

	require.NoError(t, err)
	require.Len(t, output.Tasks, 1)
	require.Len(t, llm.calls, 2, "the second attempt must run")

	retryMessages := llm.calls[1]
	last := retryMessages[len(retryMessages)-1]
	assert.Equal(t, domain.RoleUser, last.Role)
	assert.Contains(t, last.Content, "Your plan was rejected")
	assert.Contains(t, last.Content, "parallel_group 0",
		"the model needs the actual rule it broke, not a generic parse complaint")
	assert.Equal(t, rejected, retryMessages[len(retryMessages)-2].Content,
		"its own rejected plan must precede the correction")
}

// The planner used to send domain.JSONResponseFormat() — bare
// {"type":"json_object"}, no schema — so a provider with strict structured
// output support had nothing to constrain decoding against, and every stray
// comma or dropped field fell through to the expensive parse-repair retry
// loop this file otherwise exercises. The request must now carry the real
// schema instead.
func TestPlannerGenerate_RequestCarriesTheJSONSchema(t *testing.T) {
	agentID := uuid.New()
	accepted := `{"ready":true,"summary":"plan","questions":[],"tasks":[
		{"id":"t1","title":"Create","description":"Open the task","agent_id":"` + agentID.String() + `","skill_ids":[],"tool_names":["create_board_task"],"subtask_rules":[],"depends_on":[],"parallel_group":0}]}`

	llm := &scriptedLLM{responses: []string{accepted}}
	catalog := singleAgentCatalog{agent: domain.Agent{ID: agentID, Name: "product-manager", Enabled: true}}
	planner := orchestrator.NewPlanner(llm, catalog, nil, 10, 5)

	_, err := planner.Generate(
		context.Background(),
		domain.GoalIntake{Ready: true, Purpose: "p", Goal: "g"},
		"siteye android linki ekle",
		nil, "test-model", nil,
		orchestrator.PlannerOptions{Lang: "tr"},
	)

	require.NoError(t, err)
	require.Len(t, llm.requests, 1, "no parse/validation failure to retry")

	rf := llm.requests[0].ResponseFormat
	require.NotNil(t, rf, "the planner must request structured output")
	assert.Equal(t, domain.ResponseFormatJSONSchema, rf.Type)
	assert.NotEmpty(t, rf.Name, "some providers reject an unnamed schema")
	require.NotNil(t, rf.Schema)
	assert.Equal(t, "object", rf.Schema["type"])
	props, ok := rf.Schema["properties"].(map[string]interface{})
	require.True(t, ok, "schema must declare properties")
	assert.Contains(t, props, "tasks")
	assert.Contains(t, props, "questions")
}

// The planner states the disjoint-write rule as a hard, countable constraint —
// prose about "disjoint deliverables" alone produced five board writers in one
// wave.
func TestBuildPlannerSystemPrompt_StatesBoardWriteConstraintAsCheckable(t *testing.T) {
	p := orchestrator.BuildPlannerSystemPromptWithOptionsForTest(orchestrator.PlannerOptions{Lang: "tr"})

	assert.Contains(t, p, "AT MOST ONE subtask may list a board-write tool")
	assert.Contains(t, p, "create_board_task, move_board_task, update_board_task")
	assert.Contains(t, p, "count the board writers per parallel_group")
}

// The ordering has to live inside the subtask: a wave per lifecycle step plans
// work the control plane already does, and the plan validators reject it. What
// the description must carry instead is the order and the boundary.
func TestBuildPlannerSystemPrompt_ShapesSubtaskDescriptionAsOrderedPhases(t *testing.T) {
	p := orchestrator.BuildPlannerSystemPromptWithOptionsForTest(orchestrator.PlannerOptions{Lang: "tr"})

	assert.Contains(t, p, "Out of scope")
	assert.Contains(t, p, "Verify")
	assert.Contains(t, p, "never separate subtasks, never separate parallel_groups")
	assert.Contains(t, p, "pulling the repository, creating the branch, claiming the task, moving columns")
}

// Subtasks in one wave share the branch but not the context: the one adding a
// button cannot see the one that deleted a section of the same page, and both
// report success. Only a pass over the merged result catches that.
func TestBuildPlannerSystemPrompt_RequiresVerificationSubtaskOnSplitPlans(t *testing.T) {
	p := orchestrator.BuildPlannerSystemPromptWithOptionsForTest(orchestrator.PlannerOptions{Lang: "tr"})

	assert.Contains(t, p, "Final verification subtask")
	assert.Contains(t, p, "depends_on every implementing subtask")
	assert.Contains(t, p, "read the complete branch diff")
	assert.Contains(t, p, "Do not give it move_board_task")
}

// The five-subtask plan was one subtask per board column (analiz, implement,
// QA, pm_uat, approval) for a single small change.
func TestBuildPlannerSystemPrompt_RejectsOneSubtaskPerLifecycleStage(t *testing.T) {
	p := orchestrator.BuildPlannerSystemPromptWithOptionsForTest(orchestrator.PlannerOptions{Lang: "tr"})

	assert.Contains(t, p, "Do not plan one subtask per delivery lifecycle stage")
	assert.Contains(t, strings.ToLower(p), "smallest plan that satisfies the request")
}

func (c singleAgentCatalog) ListTechStacksByAgent(_ context.Context, _ uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
