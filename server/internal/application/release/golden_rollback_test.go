package release

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGoldenWriteRollbackProposalMessage pins writeRollbackProposal's exact
// comment, byte-for-byte, before it moves into
// catalog/system/prompts/briefs/**. dispatch and on_merge modes read
// differently (dispatchDescription), so both are covered.
func TestGoldenWriteRollbackProposalMessage(t *testing.T) {
	cases := []struct {
		name string
		mode domain.DeliveryMode
		want string
	}{
		{
			name: "dispatch mode redeploys the previous good release",
			mode: domain.DeliveryDispatch,
			want: "Rollback PROPOSED, not executed — auto_rollback is off for this component.\n\n" +
				"Reason: verify_failed. smoke check failed\n\n" +
				"What would happen: revert abcdef0's merge commit(s) on the default branch, then redeploy the previous good release.\n\n" +
				"A human has to confirm it (POST the rollback endpoint with the repository name as the confirmation phrase).",
		},
		{
			name: "on_merge mode's revert push itself redeploys",
			mode: domain.DeliveryOnMerge,
			want: "Rollback PROPOSED, not executed — auto_rollback is off for this component.\n\n" +
				"Reason: verify_failed. smoke check failed\n\n" +
				"What would happen: revert abcdef0's merge commit(s) on the default branch, then the revert push itself redeploys production.\n\n" +
				"A human has to confirm it (POST the rollback endpoint with the repository name as the confirmation phrase).",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRollbackFixture()
			repositoryID := uuid.New()
			componentID := uuid.New()
			task := f.withTask(repositoryID, "T-1", mergeSHA)
			created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, tc.mode, false), []uuid.UUID{task.ID})
			require.NoError(t, err)

			_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackVerifyFailed, "smoke check failed")
			require.Error(t, err)
			require.Len(t, f.tasks.comments, 1)
			require.Equal(t, tc.want, f.tasks.comments[0].Content)
		})
	}
}

// TestGoldenRollbackReopenComment pins rollbackReopenComment's exact
// wording, byte-for-byte.
func TestGoldenRollbackReopenComment(t *testing.T) {
	t.Run("with a note and early-stop evidence", func(t *testing.T) {
		r := domain.Release{
			Version: "abcdef0",
			Rollback: &domain.ReleaseRollback{
				Reason:    domain.RollbackHealthIncident,
				Note:      "error rate spiked to 12%",
				RevertSHA: "9999999999999999999999999999999999999999",
			},
			Checks: domain.ReleaseChecks{EarlyStop: "smoke check failed twice"},
		}
		want := "Release abcdef0 was rolled back (health_incident). error rate spiked to 12% Evidence: smoke check failed twice.\n\n" +
			"Your change was reverted on the default branch as 999999999999. Re-apply your change on the task branch (git revert 999999999999), fix it, and send it through review again."
		require.Equal(t, want, rollbackReopenComment(r))
	})

	t.Run("no note, no early-stop evidence", func(t *testing.T) {
		r := domain.Release{
			Version: "abcdef0",
			Rollback: &domain.ReleaseRollback{
				Reason:    domain.RollbackDeployFailed,
				RevertSHA: "9999999999999999999999999999999999999999",
			},
		}
		want := "Release abcdef0 was rolled back (deploy_failed).\n\n" +
			"Your change was reverted on the default branch as 999999999999. Re-apply your change on the task branch (git revert 999999999999), fix it, and send it through review again."
		require.Equal(t, want, rollbackReopenComment(r))
	})
}
