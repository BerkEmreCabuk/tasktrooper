package core

import "testing"

// TestPromptGoldenBeforeMove pins ToolManifest's exact byte output, before it
// moves into catalog/system/prompts/cli/tool_manifest.md.
func TestPromptGoldenBeforeMove(t *testing.T) {
	cases := []struct {
		name       string
		serverName string
		toolNames  []string
		want       string
	}{
		{
			"empty",
			"tasktrooper",
			nil,
			"",
		},
		{
			"one",
			"tasktrooper",
			[]string{"set_criterion_completed"},
			"TaskTrooper's own tools reach you through the `tasktrooper` MCP server, so their real names carry the `mcp__tasktrooper__` prefix: the tool this system's instructions call `set_criterion_completed` is called as `mcp__tasktrooper__set_criterion_completed`. They are already available to you — do NOT search for them and do not report one as missing because an unprefixed name did not resolve. These are the ones this run has, in full:\nmcp__tasktrooper__set_criterion_completed.",
		},
		{
			"several",
			"tasktrooper",
			[]string{"set_criterion_completed", "get_task", "list_criteria"},
			"TaskTrooper's own tools reach you through the `tasktrooper` MCP server, so their real names carry the `mcp__tasktrooper__` prefix: the tool this system's instructions call `set_criterion_completed` is called as `mcp__tasktrooper__set_criterion_completed`. They are already available to you — do NOT search for them and do not report one as missing because an unprefixed name did not resolve. These are the ones this run has, in full:\nmcp__tasktrooper__set_criterion_completed, mcp__tasktrooper__get_task, mcp__tasktrooper__list_criteria.",
		},
		{
			"differentServerName",
			"tt-dev",
			[]string{"ping"},
			"TaskTrooper's own tools reach you through the `tt-dev` MCP server, so their real names carry the `mcp__tt-dev__` prefix: the tool this system's instructions call `set_criterion_completed` is called as `mcp__tt-dev__set_criterion_completed`. They are already available to you — do NOT search for them and do not report one as missing because an unprefixed name did not resolve. These are the ones this run has, in full:\nmcp__tt-dev__ping.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToolManifest(tc.serverName, tc.toolNames)
			if got != tc.want {
				t.Errorf("mismatch:\n got:  %q\n want: %q", got, tc.want)
			}
		})
	}
}
