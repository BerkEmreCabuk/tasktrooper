package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repairPlanFixture(agentID uuid.UUID, taskID string, dependsOn []string) domain.PlannerOutput {
	return domain.PlannerOutput{
		Ready:     true,
		Summary:   "repair",
		Questions: []domain.ClarificationQuestion{},
		Tasks: []domain.PlannerTask{{
			ID: taskID, Title: "Fix the gap", Description: "d",
			AgentID: agentID.String(), ToolNames: []string{"run_terminal"},
			DependsOn: dependsOn,
		}},
	}
}

// A repair plan is allowed to depend on a prior plan's task: validation must know those ids.
func TestValidateRepairPlan_AcceptsDependencyOnAPriorPlanTask(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "backend-developer", Enabled: true}}
	prior := []domain.PlannerTask{{
		ID: "t1", Title: "Ship it", Description: "d",
		AgentID: agentID.String(), ToolNames: []string{"run_terminal"},
	}}
	repair := repairPlanFixture(agentID, "r1", []string{"t1"})

	err := orchestrator.ValidateRepairPlanForTest(repair, agents, nil, 10, prior)

	assert.NoError(t, err, "the prior plan's tasks are known ids for a repair plan")
}

func TestValidatePlannerOutput_FirstPlanStillRejectsForwardReferences(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "backend-developer", Enabled: true}}

	err := orchestrator.ValidatePlannerOutputForTest(repairPlanFixture(agentID, "r1", []string{"t1"}), agents, nil, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "depends on unknown task t1")
}

func TestValidateRepairPlan_StillRejectsHallucinatedDependencies(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "backend-developer", Enabled: true}}
	prior := []domain.PlannerTask{{
		ID: "t1", Title: "Ship it", Description: "d",
		AgentID: agentID.String(), ToolNames: []string{"run_terminal"},
	}}

	err := orchestrator.ValidateRepairPlanForTest(repairPlanFixture(agentID, "r1", []string{"t9"}), agents, nil, 10, prior)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "depends on unknown task t9")
}

// Prior ids are dependency targets: reusing one would collide with the plan_tasks row it names.
func TestValidateRepairPlan_RejectsReusingAPriorTaskID(t *testing.T) {
	agentID := uuid.New()
	agents := []domain.Agent{{ID: agentID, Name: "backend-developer", Enabled: true}}
	prior := []domain.PlannerTask{{
		ID: "t1", Title: "Ship it", Description: "d",
		AgentID: agentID.String(), ToolNames: []string{"run_terminal"},
	}}

	err := orchestrator.ValidateRepairPlanForTest(repairPlanFixture(agentID, "t1", nil), agents, nil, 10, prior)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reuses an id from the prior plan")
}

type flakyLLM struct {
	failures int
	err      error
	answer   string
	calls    int
}

func (f *flakyLLM) Chat(_ context.Context, _ domain.AgentRequest) (domain.AgentResponse, error) {
	f.calls++
	if f.calls <= f.failures {
		return domain.AgentResponse{}, f.err
	}
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: f.answer}}, nil
}

func (f *flakyLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (f *flakyLLM) Models(_ context.Context) ([]string, error) { return nil, nil }
func (f *flakyLLM) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, nil
}

// Transient failures wait on the same curve the agent loop uses (llmretry); these check each stage is wired to it.
func TestPipelineStage_WaitsBeforeRetryingATransientFailure(t *testing.T) {
	llm := &flakyLLM{
		failures: 1,
		err:      &domain.LLMHTTPError{StatusCode: 429, Body: "slow down"},
		answer:   `{"ready":true,"purpose":"p","goal":"g","constraints":[],"questions":[]}`,
	}

	start := time.Now()
	intake, err := orchestrator.NewIntakeExtractor(llm).
		Extract(context.Background(), "siteye android linki ekle", nil, "test-model", orchestrator.IntakeOptions{Lang: "tr"})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, intake.Ready)
	assert.Equal(t, 2, llm.calls, "the retry still happens")
	assert.GreaterOrEqual(t, elapsed, 400*time.Millisecond,
		"the second request must not leave in the same millisecond as the rejected one")
}

// A rejection the provider will repeat is not worth three identical attempts.
func TestPipelineStage_StopsImmediatelyOnANonRetryableRejection(t *testing.T) {
	llm := &flakyLLM{
		failures: 99,
		err:      &domain.LLMHTTPError{StatusCode: 400, Body: "unknown model"},
	}

	start := time.Now()
	_, err := orchestrator.NewVerifier(llm).Evaluate(
		context.Background(), domain.GoalIntake{Purpose: "p", Goal: "g"},
		"siteye android linki ekle", map[string]string{"t1": "done"}, "test-model", "",
	)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown model")
	assert.Equal(t, 1, llm.calls, "the same bytes get the same rejection; sending them again only delays it")
	assert.Less(t, elapsed, 250*time.Millisecond, "a stop must not sleep on the way out")
}

// Only the agent loop fixes an overflow by shrinking the conversation; this package's prompt is rebuilt fresh, so it is terminal.
func TestPipelineStage_StopsOnAContextOverflowRejection(t *testing.T) {
	llm := &flakyLLM{
		failures: 99,
		err:      &domain.LLMHTTPError{StatusCode: 400, Body: "This model's maximum context length is 8192 tokens"},
	}

	_, err := orchestrator.NewVerifier(llm).Evaluate(
		context.Background(), domain.GoalIntake{Purpose: "p", Goal: "g"},
		"siteye android linki ekle", map[string]string{"t1": "done"}, "test-model", "",
	)

	require.Error(t, err)
	assert.Equal(t, 1, llm.calls)
}

// A cancelled run never sleeps; retrying in a dead context only collects errors.
func TestPipelineStage_DoesNotWaitOnACancelledRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	llm := &flakyLLM{failures: 99, err: context.Canceled}

	start := time.Now()
	_, err := orchestrator.NewVerifier(llm).Evaluate(
		ctx, domain.GoalIntake{Purpose: "p", Goal: "g"},
		"siteye android linki ekle", map[string]string{"t1": "done"}, "test-model", "",
	)

	require.Error(t, err)
	assert.Equal(t, 1, llm.calls)
	assert.Less(t, time.Since(start), 250*time.Millisecond)
}
