package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGoldenOrderNote pins OrderNote's exact generated block, byte-for-byte,
// before its prose moves into catalog/system/prompts/briefs/** (rendered at
// the application consumer, since domain must not import application).
func TestGoldenOrderNote(t *testing.T) {
	cases := []struct {
		name        string
		deployAfter []string
		workAfter   []string
		want        string
	}{
		{
			name: "no relations at all",
			want: "",
		},
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
			require.Equal(t, tc.want, domain.OrderNote(tc.deployAfter, tc.workAfter))
		})
	}
}

// TestGoldenTaskRollbackRunbook pins TaskRollbackRunbook's exact wording,
// byte-for-byte, before its prose moves into
// catalog/system/prompts/briefs/** (rendered at the application consumer).
func TestGoldenTaskRollbackRunbook(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name string
		task domain.BoardTask
		want string
	}{
		{
			name: "no runbook fields at all",
			task: domain.BoardTask{},
			want: "",
		},
		{
			name: "rollback plan only",
			task: domain.BoardTask{RollbackPlan: strPtr("Turn off the `new_pricing` flag.")},
			want: "Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\nTurn off the `new_pricing` flag.",
		},
		{
			name: "before and after deploy only",
			task: domain.BoardTask{
				BeforeDeploy: strPtr("Ran the pricing_tier migration."),
				AfterDeploy:  strPtr("Flipped the `new_pricing` flag on for 10% of traffic."),
			},
			want: "What had to happen BEFORE this was deployed (each of these may need undoing, in reverse order):\nRan the pricing_tier migration.\n\n" +
				"What was done AFTER the deploy (undo anything here that is now pointing at code that no longer exists):\nFlipped the `new_pricing` flag on for 10% of traffic.",
		},
		{
			name: "all three fields",
			task: domain.BoardTask{
				RollbackPlan: strPtr("Turn off the `new_pricing` flag, then revert."),
				BeforeDeploy: strPtr("Ran the pricing_tier migration."),
				AfterDeploy:  strPtr("Flipped the `new_pricing` flag on for 10% of traffic."),
			},
			want: "Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\nTurn off the `new_pricing` flag, then revert.\n\n" +
				"What had to happen BEFORE this was deployed (each of these may need undoing, in reverse order):\nRan the pricing_tier migration.\n\n" +
				"What was done AFTER the deploy (undo anything here that is now pointing at code that no longer exists):\nFlipped the `new_pricing` flag on for 10% of traffic.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, domain.TaskRollbackRunbook(tc.task))
			require.Equal(t, tc.want != "", domain.HasRollbackRunbook(tc.task))
		})
	}
}
