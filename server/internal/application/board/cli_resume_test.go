package board

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestCriteriaSweepFollowUpResumesTheMainRunsCLISession(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp: domain.AgentResponse{
			Message:      domain.Message{Content: "implemented"},
			CLISessionID: "sess-main",
		},
	}
	criteria := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "the gate refuses a red build"}}
	updater := &settlingCriteriaUpdater{criteria: criteria, settleAfter: 2}
	r, job := criteriaSweepRunner(t, runs, ex, updater)

	require.NoError(t, r.execute(context.Background(), context.Background(), func() {}, job))

	reqs := ex.requests()
	require.Len(t, reqs, 2, "one main call plus exactly one settling sweep round")
	main, followUp := reqs[0], reqs[1]

	require.Empty(t, main.ResumeSessionID, "the main run has no prior session to resume")

	require.Equal(t, "sess-main", followUp.ResumeSessionID,
		"the sweep round must resume the session the main executor call returned")
	require.Equal(t, criteriaSweepPrompt(criteria, 1), followUp.Prompt,
		"only the sweep's new instruction is sent on a resume, not the whole conversation")
	require.Equal(t, followUp.Prompt, followUp.History[len(followUp.History)-1].Content,
		"the full history still travels on the request even though the executor only reads the tail")
	require.Len(t, followUp.History, len(main.History)+2,
		"the sweep appends exactly the assistant close-out and its own prompt onto the main run's history")
}

func TestQuotaBlockInACriteriaSweepParksTheTaskInsteadOfFailingIt(t *testing.T) {
	runs := &recordingRunStore{}
	resumeAt := time.Now().Add(90 * time.Minute).Round(time.Second)
	ex := &criteriaSweepQuotaExecutor{
		mainResp: domain.AgentResponse{Message: domain.Message{Content: "implemented"}, CLISessionID: "sess-main"},
		sweepErr: &domain.QuotaBlock{ResumeAt: resumeAt, Detail: "Claude AI usage limit reached|4102444800"},
	}
	updater := &criteriaUpdater{criteria: []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "the gate refuses a red build"},
	}}
	r, job := criteriaSweepRunner(t, runs, ex, updater)
	blocker := &blockRecorder{}
	r.SetTaskBlocker(blocker)

	require.NoError(t, r.execute(context.Background(), context.Background(), func() {}, job), "a quota park is not a run failure")

	row := runs.row()
	require.NotEqual(t, domain.TaskAgentRunStatusFailed, row.Status, "parking must not spend a consecutive-failure life")
	require.True(t, resumeAt.Equal(*row.QuotaResumeAt))
	require.Equal(t, "sess-main", row.CLISessionID,
		"a follow-up's quota block with no session of its own must resume the run the main call opened")

	resource, _ := blocker.parked()
	require.Equal(t, domain.ResourceClaudeCodeQuota, resource)
}

type criteriaSweepQuotaExecutor struct {
	mainResp domain.AgentResponse
	sweepErr error

	calls int
}

func (e *criteriaSweepQuotaExecutor) Supports(provider domain.LLMProviderType) bool {
	return provider == domain.LLMProviderClaudeCode
}

func (e *criteriaSweepQuotaExecutor) Execute(_ context.Context, _ domain.TaskExecution) (domain.AgentResponse, error) {
	e.calls++
	if e.calls == 1 {
		return e.mainResp, nil
	}
	return domain.AgentResponse{}, e.sweepErr
}

func TestParkOnQuotaEscalatesTheWindowOnRepeatedParksWithNoUsableResetTime(t *testing.T) {
	agentRec := claudeCodeAgent()
	currentID := uuid.New()
	priorPark := time.Now().Add(-3 * time.Hour)

	history := []domain.TaskAgentRun{
		{ID: currentID, AgentID: agentRec.ID},
		{ID: uuid.New(), AgentID: agentRec.ID, CLISessionID: "sess-loop", QuotaResumeAt: &priorPark},
		{ID: uuid.New(), AgentID: agentRec.ID, CLISessionID: "sess-loop", QuotaResumeAt: &priorPark},
	}
	runs := &recordingRunStore{prev: history}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		err:      &domain.QuotaBlock{CLISessionID: "sess-loop"},
	}
	r, job := executorRunner(t, agentRec, runs, ex)
	job.Run.ID = currentID
	r.SetTaskBlocker(&blockRecorder{})

	before := time.Now()
	require.NoError(t, r.execute(context.Background(), context.Background(), func() {}, job))

	row := runs.row()
	require.NotNil(t, row.QuotaResumeAt)
	require.WithinDuration(t, before.Add(2*time.Hour), *row.QuotaResumeAt, 10*time.Second)
}

func TestHTTPLoopRunRespectsTheAgentRecordsSessionLimits(t *testing.T) {
	runs := &recordingRunStore{}
	llm := &recordingLLM{}
	agentRec := domain.Agent{
		ID: uuid.New(), Name: "gpt", ProviderType: domain.LLMProviderOpenAI, Model: "gpt-4o",
		Effort: "high", MaxTurns: 7,
	}
	r := NewRunner(RunnerDeps{
		AgentLoop:    agent.NewLoop(llm, toollessRegistry{}, 3, 3, 16000),
		Runs:         runs,
		Catalog:      &agentCatalog{agent: agentRec},
		Repositories: oneRepoResolver{root: t.TempDir()},
	})
	r.SetWorkflows(workflowtest.Default().Reader())
	taskID := uuid.New()
	job := RunJob{
		Run:  domain.TaskAgentRun{ID: uuid.New(), TaskID: taskID, AgentID: agentRec.ID},
		Task: domain.BoardTask{ID: taskID, RepositoryID: uuid.New(), Key: "tt-50", Title: "loop run", Column: domain.TaskColumnInProgress},
	}

	require.NoError(t, r.execute(context.Background(), context.Background(), func() {}, job))

	require.NotEmpty(t, llm.requests)
	require.Equal(t, "high", llm.requests[0].Effort,
		"the agent record's effort must reach an HTTP-loop run the same way it already reaches a CLI one")
}
