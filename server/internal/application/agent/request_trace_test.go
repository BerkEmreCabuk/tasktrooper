package agent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestBuildLLMRequestPayloadRendersToolCallArguments(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "add the android link"},
		{
			Role:    domain.RoleAssistant,
			Content: "I read the file. Now I will add the link.",
			ToolCalls: []domain.ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"src/app/route.ts"}`},
			}},
		},
		{Role: domain.RoleTool, Name: "read_file", ToolCallID: "call-1", Content: "     1→export const x = 1"},
	}

	payload := agent.BuildLLMRequestPayloadForTest("devstral-latest", history, 12)

	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	rendered := string(raw)

	assert.Contains(t, rendered, "read_file")
	assert.Contains(t, rendered, `src/app/route.ts`)
	assert.Contains(t, rendered, "tool:read_file")
}

func TestBuildLLMRequestPayloadOmitsToolCallsWhenThereAreNone(t *testing.T) {
	history := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}

	raw, err := json.Marshal(agent.BuildLLMRequestPayloadForTest("m", history, 0))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "tool_calls")
}

func TestBuildLLMRequestPayloadCutsOnRuneBoundaries(t *testing.T) {
	history := []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("ş", 500)}}

	payload := agent.BuildLLMRequestPayloadForTest("m", history, 0)

	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.True(t, json.Valid(raw))
}
