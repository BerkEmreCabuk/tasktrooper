package agentfs

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestPromptGoldenBeforeMove pins the exact byte output of the "no system
// prompt, no rules" fallback line claude.go and cursor.go both fall back to,
// before it moves into catalog/system/prompts/agentfs/default_agent_role.md.
func TestPromptGoldenBeforeMove(t *testing.T) {
	cases := []struct {
		name  string
		agent string
		want  string
	}{
		{"reviewer", "Reviewer", "You are the Reviewer agent."},
		{"planner", "Planner", "You are the Planner agent."},
		{"spaced", "  QA Bot  ", "You are the QA Bot agent."},
	}

	for _, tc := range cases {
		t.Run("claude/"+tc.name, func(t *testing.T) {
			b := Bundle{Agent: domain.Agent{Name: tc.agent}}
			body := claudeAgent(slug(tc.agent), b)
			if !strings.HasSuffix(strings.TrimRight(body, "\n"), tc.want) {
				t.Errorf("claudeAgent body = %q, want to end with %q", body, tc.want)
			}
		})
		t.Run("cursor/"+tc.name, func(t *testing.T) {
			b := Bundle{Agent: domain.Agent{Name: tc.agent}}
			body := cursorAgentRule(b)
			if !strings.HasSuffix(strings.TrimRight(body, "\n"), tc.want) {
				t.Errorf("cursorAgentRule body = %q, want to end with %q", body, tc.want)
			}
		})
	}
}
