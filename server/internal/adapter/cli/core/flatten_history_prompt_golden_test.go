package core

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestFlattenHistoryPromptGoldenBeforeMove pins FlattenHistory's exact byte
// output, before its "Earlier assistant turn:" label moves into
// catalog/system/prompts/cli/claude_earlier_turn_label.md (reused from the
// claudecode adapter, which already renders the identical label from there).
func TestFlattenHistoryPromptGoldenBeforeMove(t *testing.T) {
	cases := []struct {
		name    string
		history []domain.Message
		want    string
	}{
		{
			"empty",
			nil,
			"",
		},
		{
			"userOnly",
			[]domain.Message{{Role: domain.RoleUser, Content: "Fix the bug."}},
			"Fix the bug.",
		},
		{
			"assistantLabeled",
			[]domain.Message{
				{Role: domain.RoleUser, Content: "Fix the bug."},
				{Role: domain.RoleAssistant, Content: "I read the file and found the bug."},
				{Role: domain.RoleUser, Content: "Now fix it."},
			},
			"Fix the bug.\n\nEarlier assistant turn:\nI read the file and found the bug.\n\nNow fix it.",
		},
		{
			"blankSkipped",
			[]domain.Message{
				{Role: domain.RoleUser, Content: "  "},
				{Role: domain.RoleUser, Content: "Real content."},
			},
			"Real content.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FlattenHistory(tc.history)
			if got != tc.want {
				t.Errorf("mismatch:\n got:  %q\n want: %q", got, tc.want)
			}
		})
	}
}
