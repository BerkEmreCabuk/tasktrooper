package board

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type releaseRollbackRunbookInput struct {
	IncidentTitle    string
	IncidentEnv      string
	IncidentSeverity string
	IncidentDetail   string
	MergeSHA         string
	Runbook          domain.RollbackRunbook
	AutoRollback     bool
}

var releaseRollbackRunbookKey = prompt.Define[releaseRollbackRunbookInput]("briefs.board.release_rollback_runbook", releaseRollbackRunbookInput{
	IncidentTitle: "high error rate", IncidentEnv: "prod", IncidentSeverity: "high", MergeSHA: "abc1234",
})
