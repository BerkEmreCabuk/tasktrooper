package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ActivityStore interface {
	CreateRun(ctx context.Context, sessionID *uuid.UUID, requestID, model string) (domain.SessionRun, error)
	// CompleteRun MUST leave a row that already reads 'cancelled' alone.
	CompleteRun(ctx context.Context, runID uuid.UUID, status string) error
	// CancelRun reports whether this write flipped the run; MUST decide in one
	// atomic statement so a run cannot be cancelled (or resurrected) twice.
	CancelRun(ctx context.Context, runID uuid.UUID) (bool, error)
	RunStatus(ctx context.Context, runID uuid.UUID) (string, error)
	AppendStep(ctx context.Context, runID uuid.UUID, stepType string, payload []byte) error
	ListRunsBySession(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.SessionRun, error)
	ListStepsByRun(ctx context.Context, runID uuid.UUID) ([]domain.SessionStep, error)
	ListActiveRuns(ctx context.Context) ([]domain.SessionRun, error)
}

type APIKeyStore interface {
	Create(ctx context.Context, name, keyHash, keyPrefix string, policy domain.ToolPolicy) (domain.APIKeyRecord, error)
	List(ctx context.Context) ([]domain.APIKeyRecord, error)
	Delete(ctx context.Context, name string) error
	FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error)
}
