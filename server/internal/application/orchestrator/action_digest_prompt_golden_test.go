package orchestrator_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestRenderActionDigestPromptGoldenAfterMove pins RenderActionDigest's exact
// byte output against the pre-move baseline captured by
// domain.TestSessionActionDigestPromptGoldenBeforeMove (since removed
// alongside domain.SessionActionDigest) — proving the catalog move at
// catalog/system/prompts/session/action_digest.md reproduces it byte-exact.
func TestRenderActionDigestPromptGoldenAfterMove(t *testing.T) {
	taskID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	commentID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	actions := []domain.SessionAction{
		{
			ToolName: "create_board_task", Verb: domain.ActionVerbCreated,
			EntityKind: domain.ActionEntityBoardTask, EntityID: &taskID,
			EntityKey: "TT-42", Title: "Ops konsolu", Column: "backlog", Priority: "high",
		},
		{
			ToolName: "move_board_task", Verb: domain.ActionVerbMoved,
			EntityKind: domain.ActionEntityBoardTask, EntityID: &taskID,
			EntityKey: "TT-42", Title: "Ops konsolu", Column: "sprint",
		},
		{
			ToolName: "add_task_comment", Verb: domain.ActionVerbCommented,
			EntityKind: domain.ActionEntityComment, EntityID: &commentID,
			EntityKey: "TT-42",
		},
	}

	want := "INTERNAL (never disclose to user): actions already performed in this conversation.\n" +
		"When the user refers to one of these records, act on the id below — do not create a new one.\n" +
		"- board_task created TT-42 \"Ops konsolu\" (id=11111111-1111-1111-1111-111111111111, column=backlog, priority=high)\n" +
		"- board_task moved TT-42 \"Ops konsolu\" (id=11111111-1111-1111-1111-111111111111, column=sprint)\n" +
		"- task_comment commented TT-42 (id=22222222-2222-2222-2222-222222222222)"

	got := orchestrator.RenderActionDigest(actions)
	if got != want {
		t.Errorf("mismatch:\n got:  %q\n want: %q", got, want)
	}

	if got := orchestrator.RenderActionDigest(nil); got != "" {
		t.Errorf("empty actions: got %q, want \"\"", got)
	}
}
