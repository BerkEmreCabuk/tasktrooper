package mcpserver

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestPromptGoldenBeforeMove pins the exact byte output of the refusal text
// (*Server).unavailable returns to an agent cli session, before it moves
// into catalog/system/prompts/mcp/**.
func TestPromptGoldenBeforeMove(t *testing.T) {
	noTools := &Server{registry: &fakeRegistry{}}
	withPolicyTool := &Server{registry: &fakeRegistry{defs: []domain.ToolDefinition{
		{Type: "function", Function: domain.FunctionDefinition{Name: "mcp_github_create_pr"}},
	}}}

	cases := []struct {
		name string
		got  callToolResult
		want string
	}{
		{
			"askUser",
			noTools.unavailable("ask_user", Run{}),
			"ask_user is not available in a headless agent session: it parks the run waiting for a human answer, which this session cannot wait for. Decide with the information you have, or say what is missing in your final message.",
		},
		{
			"skillLoadOnDisk",
			noTools.unavailable(skillLoadTool, Run{SkillsOnDisk: true}),
			skillLoadTool + " is not served to this run: its skills are already installed in this workspace and your own skill mechanism lists them, so read the one you want from there. create_skill is still available if you need to write a new skill.",
		},
		{
			"notExposed",
			noTools.unavailable("read_file", Run{}),
			"read_file is not served here — use your own built-in tool for that (Bash, Read, Write, Edit, Grep, Glob).",
		},
		{
			"unregistered",
			noTools.unavailable("set_criterion_completed", Run{}),
			"no tool called set_criterion_completed exists. Call tools/list for the ones this run has.",
		},
		{
			"policyDenied",
			withPolicyTool.unavailable("mcp_github_create_pr", Run{}),
			"mcp_github_create_pr is not available to this run: its tool policy does not allow it.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.got.IsError {
				t.Fatalf("%s: want an error result", tc.name)
			}
			got := tc.got.Content[0].Text
			if got != tc.want {
				t.Errorf("mismatch:\n got:  %q\n want: %q", got, tc.want)
			}
		})
	}
}
