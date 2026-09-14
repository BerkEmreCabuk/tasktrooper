package activity_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordingStore struct {
	steps     []string
	runStatus string
}

func (s *recordingStore) CreateRun(_ context.Context, _ *uuid.UUID, _, _ string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New()}, nil
}

func (s *recordingStore) CompleteRun(ctx context.Context, _ uuid.UUID, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.runStatus = status
	return nil
}

func (s *recordingStore) CancelRun(ctx context.Context, _ uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.runStatus != "" {
		return false, nil
	}
	s.runStatus = domain.TaskAgentRunStatusCancelled
	return true, nil
}

func (s *recordingStore) RunStatus(context.Context, uuid.UUID) (string, error) { return "", nil }

func (s *recordingStore) AppendStep(ctx context.Context, _ uuid.UUID, stepType string, _ []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.steps = append(s.steps, stepType)
	return nil
}

func (s *recordingStore) ListRunsBySession(context.Context, uuid.UUID, int) ([]domain.SessionRun, error) {
	return nil, nil
}

func (s *recordingStore) ListStepsByRun(context.Context, uuid.UUID) ([]domain.SessionStep, error) {
	return nil, nil
}

func (s *recordingStore) ListActiveRuns(context.Context) ([]domain.SessionRun, error) {
	return nil, nil
}

func TestRecorderPersistsTerminalStateAfterContextCancelled(t *testing.T) {
	store := &recordingStore{}
	ctx, cancel := context.WithCancel(context.Background())
	runCtx, rec, err := activity.StartRun(ctx, store, nil, "req-1", "model")
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, runCtx)

	cancel()

	rec.Step("subtask_failed", map[string]string{"task_key": "t1"})
	rec.Complete("failed")

	assert.Equal(t, []string{"subtask_failed"}, store.steps)
	assert.Equal(t, "failed", store.runStatus)
}
