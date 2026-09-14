package orchestrator_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type statusCall struct {
	taskID uuid.UUID
	status string
	result string
	errMsg string
}

// statusCatalog records status writes and, like pgx, refuses one whose context
// is already done.
type statusCatalog struct {
	port.CatalogStore
	calls []statusCall
}

func (c *statusCatalog) UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status, result, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.calls = append(c.calls, statusCall{taskID: taskID, status: status, result: result, errMsg: errMsg})
	return nil
}

// DE-1: the subtask that stopped to ask for file-edit permission kept a
// spinner on the board forever. Handing the clarification back to the caller
// wrote no terminal status at all, so the row stayed "running" with nothing
// left in the process to move it.
func TestMarkTaskBlocked_WritesTerminalStatusWithTheQuestion(t *testing.T) {
	catalog := &statusCatalog{}
	planTask := domain.PlanTask{ID: uuid.New(), TaskKey: "t2"}
	req := domain.ClarificationRequest{
		Context:   "I need file edit permissions",
		Questions: []domain.ClarificationQuestion{{ID: "q1", Prompt: "Which directory may I write to?"}},
	}

	orchestrator.MarkTaskBlockedForTest(context.Background(), catalog, planTask, req, "partial output")

	require.Len(t, catalog.calls, 1)
	assert.Equal(t, planTask.ID, catalog.calls[0].taskID)
	assert.Equal(t, domain.TaskStatusFailed, catalog.calls[0].status, "failed is the resumable status; runTask re-runs anything not completed")
	assert.Equal(t, "partial output", catalog.calls[0].result)
	assert.Contains(t, catalog.calls[0].errMsg, "Which directory may I write to?")
}

// The write lands when the run is already being torn down — pod drain, client
// gone. On the run's own context it was dropped and the row stayed "running".
func TestMarkTaskBlocked_PersistsAfterRunContextCancelled(t *testing.T) {
	catalog := &statusCatalog{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	orchestrator.MarkTaskBlockedForTest(ctx, catalog, domain.PlanTask{ID: uuid.New(), TaskKey: "t2"},
		domain.ClarificationRequest{Context: "blocked"}, "")

	require.Len(t, catalog.calls, 1)
	assert.Equal(t, domain.TaskStatusFailed, catalog.calls[0].status)
}

func (c *statusCatalog) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
