package prodops

import (
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGolden_ProdopsCommentsPart2 pins the exact byte output of the
// incident-recovery and release-attribution comments the whole-server sweep
// (part 2) found still living as Go string literals — both post through
// TaskBoard.AddComment onto the incident's board task, so they are
// model-facing the same as every other system comment.
func TestGolden_ProdopsCommentsPart2(t *testing.T) {
	if got := prompt.Text(incidentRecoveredCommentKey); got != "Production recovered — the incident stopped firing. Confirm the root cause is actually fixed before closing this task." {
		t.Fatalf("got %q", got)
	}

	deployedAt := time.Now().Add(-10 * time.Minute)
	note := releaseAttributionNote(domain.ReleaseAttribution{
		TaskKey: "TT-42", Title: "Add the export", MergeSHA: "abcdef1234567890", Env: "prod", DeployedAt: deployedAt,
	}, 30*time.Minute, true)
	want := "Attributed to release TT-42 (Add the export): its merge commit abcdef123456 is what prod is running, deployed 10 min ago — " +
		"inside the 30 min post-release window. auto_rollback is ON in this release's delivery profile: the rollback is being executed."
	if note != want {
		t.Fatalf("got %q, want %q", note, want)
	}

	noteOff := releaseAttributionNote(domain.ReleaseAttribution{
		TaskKey: "TT-42", Title: "Add the export", MergeSHA: "abcdef1234567890", Env: "prod", DeployedAt: deployedAt,
	}, 30*time.Minute, false)
	wantOff := "Attributed to release TT-42 (Add the export): its merge commit abcdef123456 is what prod is running, deployed 10 min ago — " +
		"inside the 30 min post-release window. auto_rollback is OFF in this release's delivery profile: the rollback is proposed and needs a human to confirm it."
	if noteOff != wantOff {
		t.Fatalf("got %q, want %q", noteOff, wantOff)
	}

	comment := releaseAttributionCommentKey.Render(releaseAttributionCommentInput{
		Note: "NOTE", Title: "prod health check failing", Env: "prod", Severity: "critical", Detail: "connection refused",
	})
	wantComment := "Production incident inside this release's health window.\n\nNOTE\n\nIncident: prod health check failing (prod, critical)\nconnection refused"
	if comment != wantComment {
		t.Fatalf("got %q, want %q", comment, wantComment)
	}
}
