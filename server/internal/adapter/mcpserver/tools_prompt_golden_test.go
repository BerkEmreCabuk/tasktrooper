package mcpserver

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestResultToMCPPromptGoldenBeforeMove pins the exact byte output
// resultToMCP builds for a Claude Code session, before it moves into
// catalog/system/prompts/mcp/**.
func TestResultToMCPPromptGoldenBeforeMove(t *testing.T) {
	cases := []struct {
		name   string
		result domain.ToolResult
		want   string
	}{
		{
			"clarification",
			domain.ToolResult{Clarification: &domain.ClarificationRequest{}},
			"ask_user asked the human a question, which a headless agent session cannot wait for. Decide with what you have, or explain what is missing in your final message.",
		},
		{
			"resourceBlockWithDetail",
			domain.ToolResult{ResourceBlock: &domain.ResourceBlock{Resource: domain.ResourceMobileDevice, Detail: "another run has it checked out"}},
			"reserve_device is waiting on mobile_device: another run has it checked out",
		},
		{
			"resourceBlockNoDetail",
			domain.ToolResult{ResourceBlock: &domain.ResourceBlock{Resource: domain.ResourceMobileDevice}},
			"reserve_device is waiting on mobile_device: the resource is held by another run",
		},
		{
			"emptyResult",
			domain.ToolResult{Content: ""},
			"grep_code returned no output.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := "grep_code"
			if tc.result.Clarification != nil {
				name = "ask_user"
			}
			if tc.result.ResourceBlock != nil {
				name = "reserve_device"
			}
			got := resultToMCP(name, tc.result)
			if len(got.Content) == 0 || got.Content[0].Text != tc.want {
				t.Errorf("%s: got %q, want %q", tc.name, got.Content, tc.want)
			}
		})
	}
}
