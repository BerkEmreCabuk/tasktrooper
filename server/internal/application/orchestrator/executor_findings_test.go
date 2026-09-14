package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ExecutorFindingsSuite covers what a subtask's second attempt knows about its
// first.
//
// Each attempt rebuilds its whole prompt, so an agent that spent thirty turns
// locating the right files used to start the retry blind and spend thirty more
// locating them again — three attempts, one attempt's worth of progress. The
// run's activity trace outlives the loop, and this is the retry reading it back.
type ExecutorFindingsSuite struct {
	suite.Suite
}

func TestExecutorFindingsSuite(t *testing.T) {
	suite.Run(t, new(ExecutorFindingsSuite))
}

// attemptLLM answers each turn in plain text and records the tool the attempt
// is pretending to call, so the executor's own completeness check sees the same
// ledger a real run would leave.
type attemptLLM struct {
	requests []domain.AgentRequest
	// toolPerCall is the tool recorded on attempt i (by call index).
	toolPerCall []string
	replies     []string
}

func (l *attemptLLM) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	idx := len(l.requests)
	l.requests = append(l.requests, req)
	if idx < len(l.toolPerCall) && l.toolPerCall[idx] != "" {
		registry.ToolUsageFromContext(ctx).Record(l.toolPerCall[idx])
	}
	reply := "done"
	if idx < len(l.replies) {
		reply = l.replies[idx]
	}
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: reply}}, nil
}

func (l *attemptLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return l.Chat(ctx, req)
}

func (l *attemptLLM) Models(context.Context) ([]string, error) { return nil, nil }

func (l *attemptLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

// findingsRegistry hands the loop no tools: the attempts here are scripted, and
// what is under test is the prompt the second one gets.
type findingsRegistry struct{ port.ToolRegistry }

func (findingsRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition { return nil }

// findingsCatalog is the little a subtask run needs from the catalog.
type findingsCatalog struct {
	port.CatalogStore
	agent    domain.Agent
	statuses []string
}

func (c *findingsCatalog) GetAgent(context.Context, uuid.UUID) (domain.Agent, error) {
	return c.agent, nil
}

func (c *findingsCatalog) ListSkillsByAgent(context.Context, uuid.UUID) ([]domain.Skill, error) {
	return nil, nil
}

func (c *findingsCatalog) UpdateTaskStatus(_ context.Context, _ uuid.UUID, status, _, _ string) error {
	c.statuses = append(c.statuses, status)
	return nil
}

// tracedStore replays the run's trace: the subtask's own start marker, then the
// calls the first attempt made.
type tracedStore struct {
	port.ActivityStore
	steps []domain.SessionStep
}

func (s *tracedStore) CreateRun(context.Context, *uuid.UUID, string, string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New()}, nil
}

func (s *tracedStore) AppendStep(context.Context, uuid.UUID, string, []byte) error { return nil }

func (s *tracedStore) ListStepsByRun(context.Context, uuid.UUID) ([]domain.SessionStep, error) {
	return s.steps, nil
}

func traceStep(t *testing.T, kind string, payload any) domain.SessionStep {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return domain.SessionStep{StepType: kind, Payload: data}
}

// trace is one plan's activity: a sibling subtask's start, then this subtask's
// start and the calls that followed it.
func (s *ExecutorFindingsSuite) trace() []domain.SessionStep {
	t := s.T()
	return []domain.SessionStep{
		traceStep(t, "subtask_started", map[string]string{"task_key": "other", "title": "a sibling"}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "read_file", "call_id": "x", "arguments": `{"path":"not-mine.go"}`,
		}),
		traceStep(t, "subtask_started", map[string]string{"task_key": "t1", "title": "the subtask under test"}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "grep_code", "call_id": "1", "arguments": `{"pattern":"AddComment"}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "grep_code", "call_id": "1", "content": "repository/service.go:88", "is_error": false,
		}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "read_file", "call_id": "2", "arguments": `{"path":"internal/application/repository/service.go"}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "read_file", "call_id": "2", "content": "package repository", "is_error": false,
		}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "write_file", "call_id": "3", "arguments": `{"path":"internal/application/repository/comment.go"}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "write_file", "call_id": "3", "content": "written", "is_error": false,
		}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "run_terminal", "call_id": "4", "arguments": `{"command":"go test ./internal/application/repository/"}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "run_terminal", "call_id": "4", "content": "exit error: exit status 1\noutput:\nFAIL", "is_error": true,
		}),
	}
}

// runOnce drives one plan of one subtask whose first attempt only claims the
// board task, which is what buys the retry this suite is about.
func (s *ExecutorFindingsSuite) runOnce(store port.ActivityStore) *attemptLLM {
	llm := &attemptLLM{
		// Attempt 1 announces it started and produces nothing: the executor
		// spots that and grants another attempt. Attempt 2 does the work.
		toolPerCall: []string{"claim_board_task", "write_file"},
		replies: []string{
			"I claimed the task and moved it to in_progress; now I will look at the project.",
			"Wrote the comment endpoint and the tests pass.",
		},
	}
	agentID := uuid.New()
	catalog := &findingsCatalog{agent: domain.Agent{ID: agentID, Name: "general-coder", Model: "m"}}
	exec := orchestrator.NewExecutor(
		agent.NewLoop(llm, findingsRegistry{}, 3, 3, 16000),
		catalog,
		domain.OrchestrationConfig{MaxParallelTasks: 1},
	)

	ctx := context.Background()
	if store != nil {
		var err error
		ctx, _, err = activity.StartRun(ctx, store, nil, "req-1", "m")
		s.Require().NoError(err)
	}

	output := domain.PlannerOutput{Summary: "plan", Tasks: []domain.PlannerTask{{
		ID: "t1", Title: "Add the comment endpoint", Description: "write it",
		ToolNames: []string{"claim_board_task", "write_file", "run_terminal"},
	}}}
	planTasks := []domain.PlanTask{{
		ID: uuid.New(), TaskKey: "t1", Title: "Add the comment endpoint",
		Status: domain.TaskStatusPending, AgentID: &agentID,
	}}

	_, _, err := exec.Execute(ctx, uuid.New(), domain.GoalIntake{Goal: "ship it"}, output, planTasks,
		nil, "m", domain.ToolPolicy{}, "en", uuid.New(), nil)
	s.Require().NoError(err)
	return llm
}

func (s *ExecutorFindingsSuite) TestSecondAttemptCarriesTheFirstAttemptsFindings() {
	llm := s.runOnce(&tracedStore{steps: s.trace()})

	s.Require().Len(llm.requests, 2, "the incomplete first attempt must buy a second one")
	first := lastUserContent(llm.requests[0].Messages)
	second := lastUserContent(llm.requests[1].Messages)

	s.NotContains(first, agent.FindingsDigestHeader, "the first attempt has no previous attempt to describe")
	s.Contains(second, agent.FindingsDigestHeader)

	// What attempt 1 changed, read and ran — the exploration attempt 2 no longer
	// has to repeat.
	s.Contains(second, "internal/application/repository/comment.go (write_file)")
	s.Contains(second, "Files read: internal/application/repository/service.go")
	s.Contains(second, `"AddComment" (grep_code)`)
	s.Contains(second, "`go test ./internal/application/repository/` → exit status 1")
	// And attempt 1's own account of itself, which is otherwise lost when the
	// prompt is rebuilt.
	s.Contains(second, "Outcome: I claimed the task")

	// The note that was already there survives: why the attempt failed, and the
	// aggregate tool counts.
	s.Contains(second, "Previous attempt failed:")
	s.Contains(second, "claim_board_task")

	// A sibling subtask's step that landed BEFORE this subtask started is not
	// this subtask's finding.
	s.NotContains(second, "not-mine.go")
}

// Without a trace behind the run — chat orchestration on a store that keeps
// none — the retry must still work, on the note it always had.
func (s *ExecutorFindingsSuite) TestRetryWithoutATraceKeepsTheOldNote() {
	llm := s.runOnce(nil)

	s.Require().Len(llm.requests, 2)
	second := lastUserContent(llm.requests[1].Messages)
	s.NotContains(second, agent.FindingsDigestHeader)
	s.Contains(second, "Previous attempt failed:")
}

func lastUserContent(messages []domain.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

func (c *findingsCatalog) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
