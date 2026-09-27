package deploywatch_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// This file pins, byte-for-byte, every model-facing Detail/Message/error
// string in service.go before it moves into catalog/system/prompts/briefs/
// deploywatch/**. See WP13.

func TestGoldenStatusNoCommitSHA(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})

	got, err := h.svc.StatusForCommit(context.Background(), repoID, "", "")
	require.NoError(t, err)
	require.Equal(t, "No commit sha was given to watch.", got.Detail)

	got2, err := h.svc.StatusForCommitSince(context.Background(), repoID, "", "", time.Time{})
	require.NoError(t, err)
	require.Equal(t, "No commit sha was given to watch.", got2.Detail)
}

func TestGoldenStatusNoRunSinceRollback(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	since := fixed
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRunAt(1, mergeSHA, "completed", "success", since.Add(-time.Hour))},
	}

	got, err := h.svc.StatusForCommitSince(context.Background(), repoID, mergeSHA, "deploy-prod.yml", since)
	require.NoError(t, err)
	require.Equal(t, "No Actions run for abc123def456 has started since the rollback began.", got.Detail)
}

func TestGoldenStatusNoMergeCommit(t *testing.T) {
	task := releasedTask()
	task.MergeCommitSHA = ""
	h := newHarness(t, task, domain.DeployTarget{Env: domain.DeployEnvProd})

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t, "This task has no merge commit recorded — its pull request has not been merged, so nothing of it can be in production yet.", got.Detail)
}

func TestGoldenStatusNothingReportsDeploy(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t, "Nothing reports a deploy of abc123def456: no Actions run carries a deploy job for it, and no commit status or GitHub Deployment was written against it. This repository does not deploy on merge (or its deploy has not started yet and has left no trace).", got.Detail)
}

func TestGoldenStatusCouldNotReadJobs(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(90, "https://gh/run/90"))
	h.actions.jobsErr = errNoWorkflow

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t, "could not read the run's jobs", got.Detail)
}

func TestGoldenStatusJobConcluded(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t, `The deploy job "deploy" of abc123def456 concluded "failure".`, got.Detail)
}

func TestGoldenStatusRunConcluded(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRun(61, mergeSHA, "completed", "failure")},
	}
	h.actions.jobsByRun[61] = jobs(job(1, "build", "completed", "success"))

	got, err := h.svc.StatusForCommit(context.Background(), repoID, mergeSHA, "deploy-prod.yml")
	require.NoError(t, err)
	require.Equal(t, `The run for abc123def456 concluded "failure".`, got.Detail)
}

func TestGoldenStatusStillRunning(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(9, ""))
	h.actions.jobsByRun[9] = jobs(job(3, "deploy", "in_progress", ""))

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t, "The deploy of abc123def456 is still running.", got.Detail)
}

func TestGoldenStatusJobFinishedSuccessNoHealthURL(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(7, ""))
	h.actions.jobsByRun[7] = jobs(
		job(1, "test", "completed", "failure"),
		job(2, "deploy", "completed", "success"),
	)

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t,
		"The deploy job for abc123def456 finished successfully. Production is now running this task's code; watch the environment (no health_url is recorded — record one with update_deploy_target) until 2026-08-17T12:15:00Z — an incident opened before then is this release's.",
		got.Detail)
}

func TestGoldenStatusJobFinishedSuccessWithHealthURL(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd, HealthURL: "https://api.example.com/health"})
	h.actions.runsForCommit = actionRuns(actionRun(55, "https://gh/run/55"))
	h.actions.jobsByRun[55] = jobs(
		job(1, "build", "completed", "success"),
		job(2, "deploy", "completed", "success"),
	)

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t,
		"The deploy job for abc123def456 finished successfully. Production is now running this task's code; watch https://api.example.com/health until 2026-08-17T12:15:00Z — an incident opened before then is this release's.",
		got.Detail)
}

func TestGoldenStatusCommitSignalSuccess(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(12, ""))
	h.actions.jobsByRun[12] = jobs(job(4, "lint", "completed", "success"))
	h.actions.commitSignal = commitSignal(domain.DeploySignalCommitStatus, "success", "Vercel")

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t,
		"Vercel reported this deploy successful for abc123def456 (no Actions deploy job exists — this repository deploys on push). Production is now running this task's code; watch the environment (no health_url is recorded — record one with update_deploy_target) until 2026-08-17T12:15:00Z — an incident opened before then is this release's.",
		got.Detail)
}

func TestGoldenStatusCommitSignalFailureNoContext(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.commitSignal = commitSignal(domain.DeploySignalDeploymentState, "failure", "")

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t,
		"the deploy provider reported this deploy FAILED for abc123def456 (no Actions deploy job exists — this repository deploys on push).",
		got.Detail)
}

func TestGoldenStatusCommitSignalPendingWithDescription(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.commitSignal = port.CommitDeploySignal{
		Kind: domain.DeploySignalDeploymentState, State: "pending", Contexts: []string{"CI"}, Description: "waiting on approval",
	}

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	require.NoError(t, err)
	require.Equal(t,
		"CI has not finished this deploy yet for abc123def456 (no Actions deploy job exists — this repository deploys on push). waiting on approval",
		got.Detail)
}

func TestGoldenEndpointLogsNoLogsURL(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})

	_, err := h.svc.EndpointLogs(context.Background(), repoID, domain.DeployEnvProd, 0)
	require.Error(t, err)
	require.Equal(t, "no logs_url is recorded for prod — record one with update_deploy_target if this application exposes a log endpoint", err.Error())
}
