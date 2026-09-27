package release

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type rollbackProposedInput struct {
	Reason     string
	Note       string
	Version    string
	IsDispatch bool
}

var rollbackProposedKey = prompt.Define[rollbackProposedInput]("briefs.release.rollback_proposed", rollbackProposedInput{
	Reason: "verify_failed", Note: "smoke check failed", Version: "abcdef0", IsDispatch: true,
})

type rollbackReopenCommentInput struct {
	Version   string
	Reason    string
	Note      string
	EarlyStop string
	RevertSHA string
}

var rollbackReopenCommentKey = prompt.Define[rollbackReopenCommentInput]("briefs.release.rollback_reopen_comment", rollbackReopenCommentInput{
	Version: "abcdef0", Reason: "health_incident", RevertSHA: "999999999999",
})

type manualStepLabeledInput struct {
	Label   string
	Runbook domain.RollbackRunbook
}

var manualStepLabeledKey = prompt.Define[manualStepLabeledInput]("briefs.release.manual_step_labeled", manualStepLabeledInput{
	Label:   "T-1",
	Runbook: domain.RollbackRunbook{Plan: "Turn off the `new_pricing` flag."},
})
