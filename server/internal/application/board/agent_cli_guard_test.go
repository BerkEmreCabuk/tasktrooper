package board

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type connectedCLI struct {
	conn *domain.AgentCLIConnection
	err  error
}

func (c connectedCLI) Connected(_ context.Context, flavor domain.AgentCLIFlavor) (*domain.AgentCLIConnection, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.conn != nil && c.conn.Flavor == flavor {
		return c.conn, nil
	}
	return nil, nil
}

func claudeConnection() *domain.AgentCLIConnection {
	return &domain.AgentCLIConnection{Flavor: domain.AgentCLIFlavorClaude, ProviderType: domain.LLMProviderClaudeCode}
}

func cursorConnection() *domain.AgentCLIConnection {
	return &domain.AgentCLIConnection{Flavor: domain.AgentCLIFlavorCursor, ProviderType: domain.LLMProviderCursorAgent}
}

func TestRunIsRefusedWhenNoAgentCLIIsConnected(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{supports: domain.LLMProviderClaudeCode}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	r.SetAgentCLIConnections(connectedCLI{})

	err := r.execute(context.Background(), job)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAgentCLINotConnected)
	assert.Zero(t, ex.callCount(), "an unverified CLI must never be handed a task")

	row := runs.row()
	assert.Equal(t, domain.TaskAgentRunStatusFailed, row.Status)
	assert.Contains(t, row.Summary, "is not connected",
		"the reason belongs on the run row, which is what the task detail shows")
	assert.Contains(t, row.Summary, "backend-developer", "and it has to name the agent that could not run")
}

func TestRunIsRefusedWhenADifferentFlavorIsConnected(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{supports: domain.LLMProviderClaudeCode}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	r.SetAgentCLIConnections(connectedCLI{conn: cursorConnection()})

	err := r.execute(context.Background(), job)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAgentCLINotConnected)
	assert.Zero(t, ex.callCount())
}

func TestRunProceedsWhenItsOwnFlavorIsConnectedAlongsideAnother(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderCursorAgent,
		resp:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "Done."}},
	}
	agent := claudeCodeAgent()
	agent.ProviderType = domain.LLMProviderCursorAgent
	r, job := executorRunner(t, agent, runs, ex)
	r.SetAgentCLIConnections(connectedCLI{conn: cursorConnection()})

	require.NoError(t, r.execute(context.Background(), job))
	assert.Equal(t, 1, ex.callCount())
}

func TestRunProceedsWhenItsFlavorIsConnected(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "Done."}},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	r.SetAgentCLIConnections(connectedCLI{conn: claudeConnection()})

	require.NoError(t, r.execute(context.Background(), job))
	assert.Equal(t, 1, ex.callCount())
	assert.Equal(t, domain.TaskAgentRunStatusCompleted, runs.row().Status)
}

func TestUnreadableConnectionRefusesRatherThanDispatches(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{supports: domain.LLMProviderClaudeCode}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)
	r.SetAgentCLIConnections(connectedCLI{err: errors.New("connection refused")})

	err := r.execute(context.Background(), job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be read")
	assert.Zero(t, ex.callCount())
}

func TestUnwiredGuardLeavesDispatchAlone(t *testing.T) {
	runs := &recordingRunStore{}
	ex := &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "Done."}},
	}
	r, job := executorRunner(t, claudeCodeAgent(), runs, ex)

	require.NoError(t, r.execute(context.Background(), job))
	assert.Equal(t, 1, ex.callCount())
}
