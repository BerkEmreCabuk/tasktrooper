package claudecode

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestPromptGoldenBeforeMove pins the exact byte output of every LLM-facing
// string this adapter builds today, before it moves into
// catalog/system/prompts/cli/**.
func TestPromptGoldenBeforeMove(t *testing.T) {
	t.Run("maxTurnsNote", func(t *testing.T) {
		cases := []struct {
			maxTurns int
			want     string
		}{
			{20, "[The agent cli session stopped at its 20-turn budget; anything above is what it had finished by then.]"},
			{40, "[The agent cli session stopped at its 40-turn budget; anything above is what it had finished by then.]"},
			{1, "[The agent cli session stopped at its 1-turn budget; anything above is what it had finished by then.]"},
		}
		for _, tc := range cases {
			got := maxTurnsNote(tc.maxTurns)
			if got != tc.want {
				t.Errorf("maxTurnsNote(%d):\n got:  %q\n want: %q", tc.maxTurns, got, tc.want)
			}
		}
	})

	t.Run("continuePrompt", func(t *testing.T) {
		cases := []struct {
			name string
			req  domain.TaskExecution
			want string
		}{
			{
				"withTitle",
				domain.TaskExecution{TaskKey: "tt-123", TaskTitle: "Add a link"},
				"The usage limit that interrupted you has reset. Continue tt-123 Add a link from where you stopped in this same workspace: finish the remaining work, then reply with a short summary of what you changed.",
			},
			{
				"keyOnly",
				domain.TaskExecution{TaskKey: "tt-9"},
				"The usage limit that interrupted you has reset. Continue tt-9 from where you stopped in this same workspace: finish the remaining work, then reply with a short summary of what you changed.",
			},
			{
				"empty",
				domain.TaskExecution{},
				"The usage limit that interrupted you has reset. Continue this task from where you stopped in this same workspace: finish the remaining work, then reply with a short summary of what you changed.",
			},
		}
		for _, tc := range cases {
			got := continuePrompt(tc.req)
			if got != tc.want {
				t.Errorf("continuePrompt/%s:\n got:  %q\n want: %q", tc.name, got, tc.want)
			}
		}
	})

	t.Run("flattenHistory/earlierAssistantTurnLabel", func(t *testing.T) {
		_, prompt := flattenHistory([]domain.Message{
			{Role: domain.RoleAssistant, Content: "I read the file and found the bug."},
			{Role: domain.RoleUser, Content: "Now fix it."},
		})
		want := "Earlier assistant turn:\nI read the file and found the bug.\n\nNow fix it."
		if prompt != want {
			t.Errorf("flattenHistory prompt:\n got:  %q\n want: %q", prompt, want)
		}
	})
}
