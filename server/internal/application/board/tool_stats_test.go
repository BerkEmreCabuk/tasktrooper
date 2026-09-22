package board

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

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

func TestStampToolStatsIsNilSafe(t *testing.T) {
	var run domain.TaskAgentRun
	stampToolStats(&run, nil)

	assert.Zero(t, run.ToolCalls)
	assert.Zero(t, run.ToolErrors)
	assert.Empty(t, run.ErrorPattern)
}

func TestFailuresDoNotCountAsUsage(t *testing.T) {
	usage := registry.NewToolUsage()
	usage.RecordError("read_file")

	assert.False(t, usage.UsedAny("read_file"))
	assert.Zero(t, usage.Count("read_file"))

	calls, failures := usage.Totals()
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, failures)
}

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

func TestPreviousRunFailuresMessageSkipsTheCurrentRun(t *testing.T) {
	current := uuid.New()
	runs := []domain.TaskAgentRun{
		{ID: current, Status: domain.TaskAgentRunStatusFailed, ErrorPattern: "edit_file failed 4×"},
	}

	assert.Empty(t, previousRunFailuresMessage(runs, current))
}
