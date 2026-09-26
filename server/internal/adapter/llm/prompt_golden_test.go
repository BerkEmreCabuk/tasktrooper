package llm

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestPromptGoldenBeforeMove pins the exact byte output of every LLM-facing
// string this package builds today (tool_calls.go, anthropic.go), before it
// moves into catalog/system/prompts/llm/**.
func TestPromptGoldenBeforeMove(t *testing.T) {
	t.Run("carriedImagesNote", func(t *testing.T) {
		cases := []struct {
			n    int
			want string
		}{
			{1, "\n[1 screenshot(s) attached — they are in the message right after this tool batch]"},
			{2, "\n[2 screenshot(s) attached — they are in the message right after this tool batch]"},
			{5, "\n[5 screenshot(s) attached — they are in the message right after this tool batch]"},
		}
		for _, tc := range cases {
			got := carriedImagesNote(tc.n)
			if got != tc.want {
				t.Errorf("carriedImagesNote(%d):\n got:  %q\n want: %q", tc.n, got, tc.want)
			}
		}
	})

	t.Run("toolImagePreamble", func(t *testing.T) {
		cases := []struct {
			n    int
			want string
		}{
			{
				1,
				"Here is the 1 screenshot(s) your last tool call captured. Look at it and judge what is actually rendered — broken images, missing assets, overlapping or clipped text, a control that is not where it should be. If you cannot see images at all, say exactly that and do not give a visual verdict: an invented description of a screenshot you never received is worse than no screenshot.",
			},
			{
				2,
				"Here are the 2 screenshot(s) your last tool call captured. Look at them and judge what is actually rendered — broken images, missing assets, overlapping or clipped text, a control that is not where it should be. If you cannot see images at all, say exactly that and do not give a visual verdict: an invented description of a screenshot you never received is worse than no screenshot.",
			},
			{
				4,
				"Here are the 4 screenshot(s) your last tool call captured. Look at them and judge what is actually rendered — broken images, missing assets, overlapping or clipped text, a control that is not where it should be. If you cannot see images at all, say exactly that and do not give a visual verdict: an invented description of a screenshot you never received is worse than no screenshot.",
			},
		}
		for _, tc := range cases {
			got := toolImagePreamble(tc.n)
			if got != tc.want {
				t.Errorf("toolImagePreamble(%d):\n got:  %q\n want: %q", tc.n, got, tc.want)
			}
		}
	})

	t.Run("jsonOnlyInstruction", func(t *testing.T) {
		req := buildAnthropicRequest("claude", nil, nil, false, domain.JSONResponseFormat(), 0)
		blocks, ok := req.System.([]anthropicSystemBlock)
		if !ok || len(blocks) != 1 {
			t.Fatalf("System = %#v, want one anthropicSystemBlock", req.System)
		}
		want := "Respond with a single valid JSON object only. No prose, no markdown code fences."
		if blocks[0].Text != want {
			t.Errorf("System text:\n got:  %q\n want: %q", blocks[0].Text, want)
		}
	})
}
