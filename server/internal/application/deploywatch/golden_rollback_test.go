package deploywatch_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestGoldenRollbackProposalMessage pins proposeRollback's exact comment,
// byte-for-byte, before it moves into catalog/system/prompts/briefs/**.
func TestGoldenRollbackProposalMessage(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: false})

	res, err := h.svc.Rollback(context.Background(), rollbackReq())
	require.NoError(t, err)
	require.True(t, res.Proposed)

	want := "Rollback PROPOSED, not executed — auto_rollback is off for prod.\n\n" +
		"What went wrong: deploy job failed on the migration step (deploy_failed).\n" +
		"What is live: abc123def456, merged from T-7.\n" +
		"Proposed action: roll prod back off this commit.\n\n" +
		"A human has to confirm it: POST /v1/repositories/" + repoID.String() + "/deploy/prod/rollback with the repository name as the confirmation phrase. " +
		"Turning on auto_rollback for this target is what would let this be done automatically next time.\n\n" +
		"Even once the code is rolled back, these are not automatic:\n" +
		"- This task changed the DATABASE SCHEMA. Reverting the code does NOT reverse the migration — the schema is still whatever the release left it as. Say so explicitly on the task and name who has to reverse it; do not report the rollback as complete.\n" +
		"- The task recorded its own rollback instructions. Perform each of them yourself and report what you did, or say clearly which ones you could not:\n" +
		"Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\n" +
		"Turn the `new_pricing` flag off, then revert. The migration adding pricing_tier must be dropped by hand."
	require.Equal(t, want, res.Message)
	require.Contains(t, h.comments.all(), want)
}

// TestGoldenRollbackWorkflowMechanismReport pins the workflow_dispatch
// rollback's exact "MECHANICAL half" report.
func TestGoldenRollbackWorkflowMechanismReport(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})
	h.rollback.dispatch = domain.DeployDispatch{
		Ref: "rollback/prod/1700000000", WorkflowFile: "deploy-prod.yml",
		RollbackOfSHA: "feedfacefeedfacefeedfacefeedfacefeedface",
	}

	res, err := h.svc.Rollback(context.Background(), rollbackReq())
	require.NoError(t, err)
	require.True(t, res.RolledBack)

	wantMessage := "Rolled back prod by dispatching deploy-prod.yml at feedfacefeed (tag rollback/prod/1700000000), returning production to feedfacefeed."
	require.Equal(t, wantMessage, res.Message)

	wantReport := wantMessage + "\n\nThis is the MECHANICAL half only. The following were NOT undone by it:\n- " +
		"This task changed the DATABASE SCHEMA. Reverting the code does NOT reverse the migration — the schema is still whatever the release left it as. Say so explicitly on the task and name who has to reverse it; do not report the rollback as complete.\n- " +
		"The task recorded its own rollback instructions. Perform each of them yourself and report what you did, or say clearly which ones you could not:\n" +
		"Rollback plan recorded on this task (FOLLOW IT — it is the developer's own instruction):\n" +
		"Turn the `new_pricing` flag off, then revert. The migration adding pricing_tier must be dropped by hand."
	require.Contains(t, h.comments.all(), wantReport)
}

// TestGoldenRollbackRevertMechanismReport pins the git-revert rollback's
// exact message when the repository has no deploy workflow.
func TestGoldenRollbackRevertMechanismReport(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})
	h.rollback.err = errNoWorkflow
	h.git.hasGit = true
	h.git.revertSHA = "9999999999999999999999999999999999999999"

	res, err := h.svc.Rollback(context.Background(), rollbackReq())
	require.NoError(t, err)
	require.True(t, res.RolledBack)

	wantMessage := "Rolled back prod by reverting abc123def456 on the default branch and pushing (999999999999). " +
		"This repository has no deploy workflow — it deploys on push, so the revert commit IS the rollback deploy."
	require.Equal(t, wantMessage, res.Message)
}

// TestGoldenRollbackFailureMessage pins the comment posted when neither
// mechanism can roll back at all.
func TestGoldenRollbackFailureMessage(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})
	h.rollback.err = errNoWorkflow
	h.git.hasGit = false

	_, err := h.svc.Rollback(context.Background(), rollbackReq())
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrRollbackNoMechanism)

	want := "Rollback FAILED: " + err.Error() + "\n\nProduction is still running this release. Escalate to a human now."
	require.Contains(t, h.comments.all(), want)
}

