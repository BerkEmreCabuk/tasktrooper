package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fixedBlockerStore struct {
	*graphRelationStore
	blockers []domain.BoardTask
}

func (f *fixedBlockerStore) ListBlockingSources(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return f.blockers, nil
}

func TestValidateMoveAllowedAllowsTodoWithAnOpenBlocker(t *testing.T) {
	svc := &Service{
		relations: &fixedBlockerStore{
			graphRelationStore: newGraphRelations(),
			blockers:           []domain.BoardTask{{ID: uuid.New(), Key: "T-5", Title: "API migration", Column: domain.TaskColumnInProgress}},
		},
		workflows: workflowtest.Default().Reader(),
	}

	require.NoError(t, svc.validateMoveAllowed(context.Background(), uuid.New(), "task", domain.TaskColumnTodo))
}

func TestValidateMoveAllowedRefusesInProgressWithAnOpenBlocker(t *testing.T) {
	svc := &Service{
		relations: &fixedBlockerStore{
			graphRelationStore: newGraphRelations(),
			blockers:           []domain.BoardTask{{ID: uuid.New(), Key: "T-5", Column: domain.TaskColumnTodo}},
		},
		workflows: workflowtest.Default().Reader(),
	}

	err := svc.validateMoveAllowed(context.Background(), uuid.New(), "task", domain.TaskColumnInProgress)

	require.Error(t, err)
	require.ErrorContains(t, err, "blocked until these are done")
	var gateErr *domain.WorkOrderGateError
	require.True(t, errors.As(err, &gateErr), "the HTTP layer needs a typed error to render task_blocked_by_dependency")
	require.Equal(t, domain.TaskColumnInProgress, gateErr.Target)
}

func TestValidateMoveAllowedAllowsBothColumnsWhenBlockerIsDone(t *testing.T) {
	svc := &Service{
		relations: &fixedBlockerStore{graphRelationStore: newGraphRelations(), blockers: nil},
		workflows: workflowtest.Default().Reader(),
	}

	require.NoError(t, svc.validateMoveAllowed(context.Background(), uuid.New(), "task", domain.TaskColumnTodo))
	require.NoError(t, svc.validateMoveAllowed(context.Background(), uuid.New(), "task", domain.TaskColumnInProgress))
}
