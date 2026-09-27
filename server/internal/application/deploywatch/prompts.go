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
