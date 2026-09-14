package board

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A run's tool counters are what the next run and the tool_error_rate KPI both
// read. They have to be written on every terminal path, so the stamp has to
// work off the shared tracker rather than off any one failure's stats.
func TestStampToolStatsCountsBothOutcomes(t *testing.T) {
	usage := registry.NewToolUsage()
	usage.Record("read_file")
	usage.Record("read_file")
	usage.RecordError("run_terminal")
	usage.RecordError("run_terminal")
	usage.RecordError("edit_file")

	var run domain.TaskAgentRun
	stampToolStats(&run, usage)

	assert.Equal(t, 5, run.ToolCalls)
	assert.Equal(t, 3, run.ToolErrors)
	assert.Equal(t, "run_terminal failed 2×, edit_file failed 1×", run.ErrorPattern)
}

// A run nobody measured stores zeros, and the KPI reads that as "no data"
// rather than as a clean run.
func TestStampToolStatsIsNilSafe(t *testing.T) {
	var run domain.TaskAgentRun
	stampToolStats(&run, nil)

	assert.Zero(t, run.ToolCalls)
	assert.Zero(t, run.ToolErrors)
	assert.Empty(t, run.ErrorPattern)
}

// The success counters must stay success-only: every gate built on ToolUsage
// asks "did the agent actually do X", and a failed call is not evidence that
// it did.
func TestFailuresDoNotCountAsUsage(t *testing.T) {
	usage := registry.NewToolUsage()
	usage.RecordError("read_file")

	assert.False(t, usage.UsedAny("read_file"))
	assert.Zero(t, usage.Count("read_file"))

	calls, failures := usage.Totals()
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, failures)
}

// The whole point is that the lesson crosses the run boundary the way the task
// branch already crosses it.
func TestPreviousRunFailuresMessage(t *testing.T) {
	current := uuid.New()
	runs := []domain.TaskAgentRun{
		{ID: current, Status: domain.TaskAgentRunStatusRunning},
		{ID: uuid.New(), Status: domain.TaskAgentRunStatusFailed, ErrorPattern: "run_terminal failed 9×"},
	}

	msg := previousRunFailuresMessage(runs, current)
	assert.Contains(t, msg, "run_terminal failed 9×")
	assert.Contains(t, msg, "Do not open with the same calls")
}

// A run that succeeded, or one that failed without its tools failing, has no
// warning to pass on — and an empty system message is noise in every prompt.
func TestPreviousRunFailuresMessageStaysQuietWithoutAPattern(t *testing.T) {
	current := uuid.New()

	assert.Empty(t, previousRunFailuresMessage(nil, current))
	assert.Empty(t, previousRunFailuresMessage([]domain.TaskAgentRun{
		{ID: uuid.New(), Status: domain.TaskAgentRunStatusCompleted, ErrorPattern: "read_file failed 2×"},
	}, current))
	assert.Empty(t, previousRunFailuresMessage([]domain.TaskAgentRun{
		{ID: uuid.New(), Status: domain.TaskAgentRunStatusFailed},
	}, current))
}

// The current run has no pattern yet and must never read its own row.
func TestPreviousRunFailuresMessageSkipsTheCurrentRun(t *testing.T) {
	current := uuid.New()
	runs := []domain.TaskAgentRun{
		{ID: current, Status: domain.TaskAgentRunStatusFailed, ErrorPattern: "edit_file failed 4×"},
	}

	assert.Empty(t, previousRunFailuresMessage(runs, current))
}
