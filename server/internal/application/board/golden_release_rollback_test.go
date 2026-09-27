package board_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mustRenderRollbackRunbook renders partial.rollback_runbook directly, so
// the golden fixture below composes the same way releaseRollbackRunbook
// does, without duplicating its wording.
func mustRenderRollbackRunbook(t *testing.T, data domain.RollbackRunbook) string {
	t.Helper()
	out, err := prompt.Default().Render("partial.rollback_runbook", data)
	require.NoError(t, err)
	return out
}

// TestGoldenReleaseRollbackRunbook pins releaseRollbackRunbook's exact
// wording, byte-for-byte, before it moves into
// catalog/system/prompts/briefs/**.
func TestGoldenReleaseRollbackRunbook(t *testing.T) {
	newDispatcher := func(task domain.BoardTask) (*board.ReleaseRollbackDispatcher, *releaseRollbackCommenter) {
		agentA := uuid.New()
		boardStore := &fakeBoardConfigStore{agentsByColumn: map[string][]uuid.UUID{"done": {agentA}}}
		disp := board.NewDispatcher(boardStore, &fakeEventStore{}, &fakeRunStore{}, &fakeRunner{}, true)
		disp.SetWorkflows(workflowtest.Default().Reader())
		commenter := &releaseRollbackCommenter{releaseRollbackTaskReader: &releaseRollbackTaskReader{task: task}}
		return board.NewReleaseRollbackDispatcher(disp, commenter), commenter
	}

	t.Run("auto_rollback on, no rollback plan recorded", func(t *testing.T) {
		task := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), TaskType: "task", Column: domain.TaskColumnDone, MergeCommitSHA: "abc1234567890"}
		rrd, commenter := newDispatcher(task)
		incident := domain.Incident{Title: "high error rate", Env: "prod", Severity: "high"}

		err := rrd.DispatchReleaseRollback(context.Background(), domain.ReleaseAttribution{TaskID: task.ID}, incident, true)
		require.NoError(t, err)
		require.Len(t, commenter.comments, 1)

		want := "ROLLBACK REQUIRED — this task's release is what production is running, and production is unhealthy.\n\n" +
			"Incident: high error rate (prod, severity high)\n" +
			"Released commit: " + domain.ShortSHA(task.MergeCommitSHA) + "\n\n" +
			"This task recorded NO rollback plan. Say so explicitly when you report — the absence is itself a finding for the next release.\n\n" +
			"auto_rollback is ON in this release's delivery profile: call rollback_release with reason=health_incident. " +
			"It will undo the code — by re-deploying the last good commit where a deploy workflow exists, or by reverting the merge commit on the default branch where the host deploys on push. " +
			"Then work through the plan above yourself and report every step you performed AND every step you could not — a schema change, a feature flag, anything with a human on the other end. " +
			"Do not report the rollback as complete unless it is."
		require.Equal(t, want, commenter.comments[0])
	})

	t.Run("auto_rollback off, incident detail present, rollback plan recorded", func(t *testing.T) {
		task := domain.BoardTask{
			ID: uuid.New(), RepositoryID: uuid.New(), TaskType: "task", Column: domain.TaskColumnReleased,
			MergeCommitSHA: "deadbeef0000", RollbackPlan: strPtr("Revert the feature flag `new_checkout`."),
		}
		rrd, commenter := newDispatcher(task)
		incident := domain.Incident{Title: "checkout errors spiking", Env: "prod", Severity: "critical", Detail: "5xx rate above 10%"}

		err := rrd.DispatchReleaseRollback(context.Background(), domain.ReleaseAttribution{TaskID: task.ID}, incident, false)
		require.NoError(t, err)
		require.Len(t, commenter.comments, 1)

		want := "ROLLBACK REQUIRED — this task's release is what production is running, and production is unhealthy.\n\n" +
			"Incident: checkout errors spiking (prod, severity critical)\n" +
			"5xx rate above 10%\n" +
			"Released commit: " + domain.ShortSHA(task.MergeCommitSHA) + "\n\n" +
			mustRenderRollbackRunbook(t, domain.TaskRollbackRunbookFields(task)) + "\n\n" +
			"auto_rollback is OFF in this release's delivery profile: call rollback_release with reason=health_incident anyway — it will execute NOTHING and return the written-up proposal (`proposed: true`). " +
			"That is the correct outcome here. Post what it returns on this task, say plainly that a human has to confirm it, and stop. Do not look for another way to roll production back. " +
			"Then work through the plan above yourself and report every step you performed AND every step you could not — a schema change, a feature flag, anything with a human on the other end. " +
			"Do not report the rollback as complete unless it is."
		require.Equal(t, want, commenter.comments[0])
	})
}

func strPtr(s string) *string { return &s }
