package release

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// TestGolden_BeforeDeployAndRollbackWording pins the exact byte output of
// before_deploy.go's and rollback.go's refusal/comment wording: it reaches
// the agent through the merge_task_pull_request gate (MergeGate),
// trigger_release/deploy_release (beforeDeployGate) and rollback_release
// (Rollback), or as a system comment posted on the task.
func TestGolden_BeforeDeployAndRollbackWording(t *testing.T) {
	assert := func(got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	assert(deliveryUnconfirmedKey.Render(releaseNameInput{Name: "api"}),
		"api's delivery profile is not confirmed. Nothing was merged. Do not retry — you will be woken when a human confirms it: confirm the delivery profile on the Deploy tab first")
	assert(prompt.Text(deliveryUnconfirmedCommentKey),
		"Waiting to merge: confirm the delivery profile on the Deploy tab first — until then nothing here knows whether merging this deploys it.")

	assert(beforeDeployPendingMergeKey.Render(releaseNameStepsInput{Name: "api", Steps: "Flip the flag."}),
		"api deploys on merge, and this task's before-deploy steps are not confirmed — a human must perform them and press \"Confirm before-deploy steps\" on the task before it can merge. Nothing was merged. Do not retry — you will be woken when a human confirms:\n\nFlip the flag.")
	assert(beforeDeployPendingMergeCommentKey.Render(releaseNameStepsInput{Name: "api", Steps: "Flip the flag."}),
		"Waiting to merge: api deploys on merge, and this task has before-deploy steps a human must perform first. Do them, then press \"Confirm before-deploy steps\" on the task:\n\nFlip the flag.")

	assert(prompt.Text(beforeDeployPendingIntroKey),
		"a human must perform these before-deploy steps and press \"Confirm before-deploy steps\" on each task before this release can deploy")
	assert(beforeDeployPendingDeployCommentKey.Render(releaseMessageInput{Message: "x\n- T-1: y"}),
		"Deploy refused: x\n- T-1: y\n\nNothing was deployed. Do not retry — you will be woken when a human confirms.")

	assert(rollbackNotAllowedReasonKey.Render(rollbackNotAllowedReasonInput{Case: "newer_shipped", Version: "1.2.0"}),
		"a newer release (1.2.0) has since shipped for this component")
	assert(rollbackNotAllowedReasonKey.Render(rollbackNotAllowedReasonInput{Case: "too_old"}),
		"this release finished more than 24 hours ago")
	assert(rollbackNotAllowedReasonKey.Render(rollbackNotAllowedReasonInput{Case: "no_released"}),
		"no released release was found for this component")
	assert(rollbackNotAllowedReasonKey.Render(rollbackNotAllowedReasonInput{Case: "unresolved", Err: "db down"}),
		"the component's newest released release could not be resolved: db down")
	assert(rollbackNotAllowedReasonKey.Render(rollbackNotAllowedReasonInput{Case: "wrong_status", Status: "draft"}),
		"this release is draft")
	assert(rollbackNotAllowedReasonKey.Render(rollbackNotAllowedReasonInput{Case: "newer_open", Version: "1.3.0"}),
		"the component has a newer open release (1.3.0) — resolve that one first")

	assert(rollbackNotAllowedKey.Render(rollbackNotAllowedInput{Why: "this release finished more than 24 hours ago"}),
		"rollback_release only applies to a failed or awaiting-verdict release, or one released within the last 24h that is still its component's newest (this release finished more than 24 hours ago)")

	assert(rollbackInvalidReasonKey.Render(rollbackInvalidReasonInput{Reason: `"weird"`}),
		`invalid rollback reason "weird" — must be deploy_failed, verify_failed, health_incident or manual`)

	assert(prompt.Text(rollbackNeedsHumanKey), "the proposal was written as a comment on the newest task for a human to confirm")
	assert(prompt.Text(rollbackNotConfiguredKey), "no git reverter is configured on this deployment")
}
