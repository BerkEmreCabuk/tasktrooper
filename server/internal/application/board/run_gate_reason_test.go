package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

// Every string runner.go actually writes to a failed run's Summary for a
// grounding-gate rejection must satisfy domain.IsRunGateRejection, or the
// gate_rejected_runs KPI silently stops counting real rejections.
func TestRunGateRejectionReasons_AllSatisfyIsRunGateRejection(t *testing.T) {
	reasons := []string{
		ungroundedAnalysisReason,
		ungroundedQAReason,
		noUIEvidenceReason,
		ungroundedPMUATReason,
		pmUncoveredCriterionReason,
	}
	for _, reason := range reasons {
		assert.True(t, domain.IsRunGateRejection(reason), reason)
	}
}
