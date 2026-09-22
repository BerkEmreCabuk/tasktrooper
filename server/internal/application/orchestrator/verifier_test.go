package orchestrator_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVerificationResult_Valid(t *testing.T) {
	raw := `{"passed":false,"issues":["missing tests"],"summary":"Incomplete"}`
	result, err := orchestrator.ParseVerificationResultForTest(raw)
	require.NoError(t, err)
	assert.False(t, result.Passed)
	assert.Equal(t, []string{"missing tests"}, result.Issues)
	assert.Equal(t, "Incomplete", result.Summary)
}

func TestParseVerificationResult_MissingIssuesField(t *testing.T) {
	_, err := orchestrator.ParseVerificationResultForTest(`{"passed":true,"summary":"ok"}`)
	assert.Error(t, err)
}

func TestParseVerificationResult_MissingSummary(t *testing.T) {
	_, err := orchestrator.ParseVerificationResultForTest(`{"passed":true,"issues":[],"summary":""}`)
	assert.Error(t, err)
}

// Open acceptance criteria on an implementation run are a gap, and repairable inside the same run.
func TestBuildVerifierSystemPrompt_OpenCriteriaOnAnImplementationRunAreIssues(t *testing.T) {
	p := orchestrator.BuildVerifierSystemPromptForTest()

	assert.Contains(t, p, "When the run IMPLEMENTED a board task")
	assert.Contains(t, p, "unsatisfied, unticked or unverified is a material gap")
	assert.Contains(t, p, "red build, a failing test, or a regression")
	assert.Contains(t, p, "missing hand-off move is still never an issue")
}

func TestBuildVerifierSystemPrompt_BoardLatencyIsNotAGap(t *testing.T) {
	p := orchestrator.BuildVerifierSystemPromptForTest()

	assert.Contains(t, p, "A chat run's deliverable is normally a RECORD")
	assert.Contains(t, p, "They are NOT verification issues.")
	assert.Contains(t, p, "An issue whose only remedy is opening another board task is not an issue")
}
