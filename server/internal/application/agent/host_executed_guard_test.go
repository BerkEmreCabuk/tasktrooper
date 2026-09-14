package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func TestLoopRefusesAHostExecutedProviderBeforeAnyHTTPCall(t *testing.T) {
	history := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}

	t.Run("buffered chat", func(t *testing.T) {
		loop := agent.NewLoop(new(mocks.LLMClient), new(mocks.ToolRegistry), 3, 3, 16000)

		_, err := loop.Run(context.Background(), history, "opus", domain.LLMProviderClaudeCode, domain.ToolPolicy{})

		requireHostExecutedRefusal(t, err)
	})

	t.Run("streamed chat", func(t *testing.T) {
		loop := agent.NewLoop(new(mocks.LLMClient), new(mocks.ToolRegistry), 3, 3, 16000)

		_, err := loop.RunStream(context.Background(), history, "opus", domain.LLMProviderClaudeCode, domain.ToolPolicy{}, func(string) {})

		requireHostExecutedRefusal(t, err)
	})

	t.Run("board task", func(t *testing.T) {
		loop := agent.NewLoop(new(mocks.LLMClient), new(mocks.ToolRegistry), 3, 3, 16000)

		_, err := loop.RunTask(context.Background(), history, "opus", domain.LLMProviderClaudeCode, domain.ToolPolicy{})

		requireHostExecutedRefusal(t, err)
	})
}

func requireHostExecutedRefusal(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)

	msg := err.Error()

	assert.Contains(t, msg, "local runner host")

	assert.NotContains(t, msg, "unsupported protocol scheme")
	assert.NotContains(t, msg, "chat/completions")
	assert.False(t, strings.Contains(msg, "attempts"),
		"a configuration that cannot work is not something to retry three times")
}
