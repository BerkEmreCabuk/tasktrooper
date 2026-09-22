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

// Refuses a status write whose context is already done, like pgx.
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

// A clarification handed back to the caller wrote no terminal status, so the row stayed "running" with no process left to move it.
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

// The status write must survive the run's own context being torn down.
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
