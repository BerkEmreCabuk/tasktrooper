package deploywatch

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type errorInput struct{ Error string }

var rollbackFailedKey = prompt.Define[errorInput]("briefs.deploywatch.rollback_failed", errorInput{Error: "context deadline exceeded"})

type rollbackDispatchedInput struct {
	Env           string
	WorkflowFile  string
	RollbackOfSHA string
	Ref           string
}

var rollbackDispatchedKey = prompt.Define[rollbackDispatchedInput]("briefs.deploywatch.rollback_dispatched", rollbackDispatchedInput{
	Env: "prod", WorkflowFile: "deploy-prod.yml", RollbackOfSHA: "feedfacefeed", Ref: "rollback/prod/1700000000",
})

type rollbackRevertedInput struct {
	Env       string
	MergeSHA  string
	RevertSHA string
}

var rollbackRevertedKey = prompt.Define[rollbackRevertedInput]("briefs.deploywatch.rollback_reverted", rollbackRevertedInput{
	Env: "prod", MergeSHA: "abc123def456", RevertSHA: "999999999999",
})

var (
	manualStepSchema = prompt.Define[struct{}]("briefs.deploywatch.manual_step_schema", struct{}{})
	manualStepNoPlan = prompt.Define[struct{}]("briefs.deploywatch.manual_step_no_plan", struct{}{})
)

type runbookInput struct{ Runbook domain.RollbackRunbook }

var manualStepRunbookKey = prompt.Define[runbookInput]("briefs.deploywatch.manual_step_runbook", runbookInput{
	Runbook: domain.RollbackRunbook{Plan: "Turn off the `new_pricing` flag, then revert."},
})

type rollbackProposedInput struct {
	Env          string
	Reason       string
	Trigger      string
	MergeSHA     string
	TaskKey      string
	RepositoryID string
	Steps        []string
}

var rollbackProposedKey = prompt.Define[rollbackProposedInput]("briefs.deploywatch.rollback_proposed", rollbackProposedInput{
	Env: "prod", Reason: "the release failed", Trigger: "deploy_failed", MergeSHA: "abc123def456", TaskKey: "T-7",
	RepositoryID: "11111111-1111-1111-1111-111111111111", Steps: []string{prompt.Text(manualStepSchema)},
})

type rollbackReportInput struct {
	Message     string
	ManualSteps []string
	NoRunbook   bool
}

var rollbackReportKey = prompt.Define[rollbackReportInput]("briefs.deploywatch.rollback_report", rollbackReportInput{
	Message: "Rolled back prod.",
})

var statusNoSHA = prompt.Define[struct{}]("briefs.deploywatch.status_no_sha", struct{}{})

type shaInput struct{ SHA string }

var statusNoRunSinceRollbackKey = prompt.Define[shaInput]("briefs.deploywatch.status_no_run_since_rollback", shaInput{SHA: "abc123def456"})

var statusNoMergeCommit = prompt.Define[struct{}]("briefs.deploywatch.status_no_merge_commit", struct{}{})

var statusNothingReportsDeployKey = prompt.Define[shaInput]("briefs.deploywatch.status_nothing_reports_deploy", shaInput{SHA: "abc123def456"})

var statusJobsUnreadable = prompt.Define[struct{}]("briefs.deploywatch.status_jobs_unreadable", struct{}{})

type jobConcludedInput struct {
	JobName    string
	SHA        string
	Conclusion string
}

var statusJobConcludedKey = prompt.Define[jobConcludedInput]("briefs.deploywatch.status_job_concluded", jobConcludedInput{
	JobName: "deploy", SHA: "abc123def456", Conclusion: "failure",
})

type runConcludedInput struct {
	SHA        string
	Conclusion string
}

var statusRunConcludedKey = prompt.Define[runConcludedInput]("briefs.deploywatch.status_run_concluded", runConcludedInput{
	SHA: "abc123def456", Conclusion: "failure",
})

var statusStillRunningKey = prompt.Define[shaInput]("briefs.deploywatch.status_still_running", shaInput{SHA: "abc123def456"})

var statusJobFinishedKey = prompt.Define[shaInput]("briefs.deploywatch.status_job_finished", shaInput{SHA: "abc123def456"})

type healthWindowNoteInput struct {
	HealthLabel string
	Until       string
}

var statusHealthWindowNoteKey = prompt.Define[healthWindowNoteInput]("briefs.deploywatch.status_health_window_note", healthWindowNoteInput{
	HealthLabel: "https://api.example.com/health", Until: "2026-08-17T12:15:00Z",
})

var statusNoHealthURL = prompt.Define[struct{}]("briefs.deploywatch.status_no_health_url", struct{}{})

type commitSignalInput struct {
	Who         string
	State       string
	SHA         string
	Description string
}

var statusCommitSignalKey = prompt.Define[commitSignalInput]("briefs.deploywatch.status_commit_signal", commitSignalInput{
	Who: "Vercel", State: "success", SHA: "abc123def456",
})

type logsEnvInput struct{ Env string }

var logsNoLogsURLKey = prompt.Define[logsEnvInput]("briefs.deploywatch.logs_no_logs_url", logsEnvInput{Env: "prod"})
