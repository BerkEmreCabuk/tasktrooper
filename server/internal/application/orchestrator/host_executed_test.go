package orchestrator_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// subtaskCLI is the Claude Code executor for one plan subtask.
type subtaskCLI struct {
	mu    sync.Mutex
	calls int
	last  domain.TaskExecution
}

func (c *subtaskCLI) Supports(p domain.LLMProviderType) bool {
	return p == domain.LLMProviderClaudeCode
}

func (c *subtaskCLI) Execute(_ context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.last = req
	return domain.AgentResponse{Message: domain.Message{
		Role: domain.RoleAssistant, Content: "Wrote the endpoint.",
	}}, nil
}

func (c *subtaskCLI) snapshot() (int, domain.TaskExecution) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.last
}

// A plan subtask assigned to an agent on a host-executed provider runs on that
// host's CLI, in the subtask's OWN workspace.
//
// Before the router this was the plainest failure of the lot: the executor
// called agentLoop.RunTask with the agent's provider, the loop refused it, and
// the subtask burned all its retries on an error that described a configuration
// problem nobody had.
func TestSubtaskRunsOnTheHostExecutor(t *testing.T) {
	cli := &subtaskCLI{}
	llm := &attemptLLM{}
	router := agent.NewRouter(agent.NewLoop(llm, findingsRegistry{}, 3, 3, 16000))
	router.SetTaskExecutor(cli)

	agentID := uuid.New()
	catalog := &findingsCatalog{agent: domain.Agent{
		ID: agentID, Name: "backend-developer",
		ProviderType: domain.LLMProviderClaudeCode, Model: "sonnet",
	}}
	exec := orchestrator.NewExecutor(router, catalog, domain.OrchestrationConfig{MaxParallelTasks: 1})

	// The session workspace the subtask carves its own directory out of.
	ctx := registry.ContextWithWorkspaceDir(context.Background(), t.TempDir())

	output := domain.PlannerOutput{Summary: "plan", Tasks: []domain.PlannerTask{{
		ID: "t1", Title: "Add the comment endpoint", Description: "write it",
		ToolNames: []string{"write_file"},
	}}}
	planTasks := []domain.PlanTask{{
		ID: uuid.New(), TaskKey: "t1", Title: "Add the comment endpoint",
		Status: domain.TaskStatusPending, AgentID: &agentID,
	}}

	summary, results, err := exec.Execute(ctx, uuid.New(), domain.GoalIntake{Goal: "ship it"}, output, planTasks,
		nil, "fallback-model", domain.ToolPolicy{}, "en", uuid.New(), nil)
	require.NoError(t, err)
	require.Contains(t, summary+strings.Join(valuesOf(results), " "), "Wrote the endpoint.")

	calls, req := cli.snapshot()
	require.Equal(t, 1, calls, "the subtask must reach the CLI once")
	require.Equal(t, domain.LLMProviderClaudeCode, req.Provider)
	require.Equal(t, "sonnet", req.Model, "the agent's own model, not the plan's fallback")
	require.Equal(t, "t1", req.TaskKey, "the CLI session is labelled with the subtask key")
	require.NotEmpty(t, req.WorkDir, "a subtask's CLI session runs in the subtask's own directory")
	require.Empty(t, llm.requests, "nothing about this subtask may reach an HTTP provider")
}

func valuesOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
