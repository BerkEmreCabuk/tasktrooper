package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ActivityStore interface {
	CreateRun(ctx context.Context, sessionID *uuid.UUID, requestID, model string) (domain.SessionRun, error)
	// CompleteRun stamps a terminal status. Implementations MUST leave a row that
	// already reads 'cancelled' alone: the goroutine unwinding behind a cancelled
	// context still writes its own verdict on the way out, and that verdict would
	// otherwise overwrite the stop the human asked for.
	CompleteRun(ctx context.Context, runID uuid.UUID, status string) error
	// CancelRun flips a running row to 'cancelled' and reports whether it was the
	// write that did it. False (the run had already stopped) is an ordinary answer,
	// not an error — the caller races the run itself. Implementations MUST decide
	// this in one atomic statement, so a double-click cannot cancel a run twice or
	// resurrect one that finished a millisecond earlier.
	CancelRun(ctx context.Context, runID uuid.UUID) (bool, error)
	// RunStatus reads one run's status. It exists so a turn can find out that
	// it was cancelled by somebody who could not reach it: the stop request is
	// served by whichever replica the load balancer picked, and only the
	// replica actually streaming the turn holds its cancel func. The row is the
	// one thing both can see, so the executing replica watches it.
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
