package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGoldenOrderNoteEmpty pins OrderNoteEmpty, the data-only check domain
// keeps; rendering the generated block's prose happens at the application
// consumer (briefs.repository.order_note in catalog/system) — see
// TestGoldenOrderNotePrompt below for that text, byte-for-byte.
func TestGoldenOrderNoteEmpty(t *testing.T) {
	require.True(t, domain.OrderNoteEmpty(nil, nil))
	require.False(t, domain.OrderNoteEmpty([]string{"T-12"}, nil))
	require.False(t, domain.OrderNoteEmpty(nil, []string{"T-9"}))
}

// TestGoldenOrderNotePrompt pins briefs.repository.order_note's exact
// wording, byte-for-byte, wrapped the way the application consumer wraps it
// (domain.OrderNoteOpen/Close around the rendered body).
func TestGoldenOrderNotePrompt(t *testing.T) {
	render := func(deployAfter, workAfter []string) string {
		out, err := prompt.Default().Render("briefs.repository.order_note", struct {
			DeployAfter []string
			WorkAfter   []string
		}{DeployAfter: deployAfter, WorkAfter: workAfter})
		require.NoError(t, err)
		return domain.OrderNoteOpen + "\n" + out + domain.OrderNoteClose
	}

	cases := []struct {
		name        string
		deployAfter []string
		workAfter   []string
		want        string
	}{
		{
			name:        "deploy-after only",
			deployAfter: []string{"T-12"},
			want: domain.OrderNoteOpen + "\n**Release order (generated from this task's relations — do not edit by hand):**\n" +
				"- Ships after: T-12. Each one must be live in production before this task is released; the release is refused otherwise.\n" +
				domain.OrderNoteClose,
		},
		{
			name:      "work-after only",
			workAfter: []string{"T-9", "T-11"},
			want: domain.OrderNoteOpen + "\n**Release order (generated from this task's relations — do not edit by hand):**\n" +
				"- Built after: T-9, T-11. Work on this task does not start until those are done.\n" +
				domain.OrderNoteClose,
		},
		{
			name:        "both deploy-after and work-after",
			deployAfter: []string{"T-12"},
			workAfter:   []string{"T-9"},
			want: domain.OrderNoteOpen + "\n**Release order (generated from this task's relations — do not edit by hand):**\n" +
				"- Ships after: T-12. Each one must be live in production before this task is released; the release is refused otherwise.\n" +
				"- Built after: T-9. Work on this task does not start until those are done.\n" +
				domain.OrderNoteClose,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, render(tc.deployAfter, tc.workAfter))
		})
	}
}

// TestGoldenTaskRollbackRunbookFields pins TaskRollbackRunbookFields' data
// extraction (trimming, and which fields are present); the rendering of
// this data into agent-facing text happens at the application consumer
// (partial.rollback_runbook in catalog/system, since domain must not import
// application) — see TestGoldenRollbackRunbookPartial below for that text,
// byte-for-byte.
func TestGoldenTaskRollbackRunbookFields(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name string
		task domain.BoardTask
		want domain.RollbackRunbook
	}{
		{
			name: "no runbook fields at all",
			task: domain.BoardTask{},
			want: domain.RollbackRunbook{},
		},
		{
			name: "rollback plan only, trimmed",
			task: domain.BoardTask{RollbackPlan: strPtr("  Turn off the `new_pricing` flag.  ")},
			want: domain.RollbackRunbook{Plan: "Turn off the `new_pricing` flag."},
		},
		{
			name: "before and after deploy only",
			task: domain.BoardTask{
				BeforeDeploy: strPtr("Ran the pricing_tier migration."),
				AfterDeploy:  strPtr("Flipped the `new_pricing` flag on for 10% of traffic."),
			},
			want: domain.RollbackRunbook{
				Before: "Ran the pricing_tier migration.",
				After:  "Flipped the `new_pricing` flag on for 10% of traffic.",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.TaskRollbackRunbookFields(tc.task)
			require.Equal(t, tc.want, got)
			require.Equal(t, !tc.want.Empty(), domain.HasRollbackRunbook(tc.task))
		})
	}
}

// TestGoldenRollbackRunbookPartial pins partial.rollback_runbook's exact
// wording, byte-for-byte, across its optional Plan/Before/After sections.
func TestGoldenRollbackRunbookPartial(t *testing.T) {
	cases := []struct {
		name string
		data domain.RollbackRunbook
		want string
	}{
		{
			name: "no fields at all",
			data: domain.RollbackRunbook{},
			want: "",
		},
		{
			name: "rollback plan only",
			data: domain.RollbackRunbook{Plan: "Turn off the `new_pricing` flag."},
			want: "Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\nTurn off the `new_pricing` flag.",
		},
		{
			name: "before and after deploy only",
			data: domain.RollbackRunbook{
				Before: "Ran the pricing_tier migration.",
				After:  "Flipped the `new_pricing` flag on for 10% of traffic.",
			},
			want: "What had to happen BEFORE this was deployed (each of these may need undoing, in reverse order):\nRan the pricing_tier migration.\n\n" +
				"What was done AFTER the deploy (undo anything here that is now pointing at code that no longer exists):\nFlipped the `new_pricing` flag on for 10% of traffic.",
		},
		{
			name: "all three fields",
			data: domain.RollbackRunbook{
				Plan:   "Turn off the `new_pricing` flag, then revert.",
				Before: "Ran the pricing_tier migration.",
				After:  "Flipped the `new_pricing` flag on for 10% of traffic.",
			},
			want: "Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\nTurn off the `new_pricing` flag, then revert.\n\n" +
				"What had to happen BEFORE this was deployed (each of these may need undoing, in reverse order):\nRan the pricing_tier migration.\n\n" +
				"What was done AFTER the deploy (undo anything here that is now pointing at code that no longer exists):\nFlipped the `new_pricing` flag on for 10% of traffic.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := prompt.Default().Render("partial.rollback_runbook", tc.data)
			require.NoError(t, err)
			require.Equal(t, tc.want, out)
		})
	}
}
