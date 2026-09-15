package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// BillingStore persists the tenant's effective plan, per-model credit prices,
// and the set of tasks paused because the budget was exhausted.
type BillingStore interface {
	GetPlan(ctx context.Context) (domain.BillingPlan, error)
	UpdatePlan(ctx context.Context, req domain.UpdateBillingPlanRequest) (domain.BillingPlan, error)
	// SetPeriodStart rolls the billing period to a new start (period renewal).
	SetPeriodStart(ctx context.Context, start time.Time) error

	ListModelPrices(ctx context.Context) ([]domain.ModelPrice, error)
	UpsertModelPrice(ctx context.Context, price domain.ModelPrice) (domain.ModelPrice, error)
	DeleteModelPrice(ctx context.Context, model string) error

	// UsdSpentSince sums token usage priced via model_prices since a time.
	UsdSpentSince(ctx context.Context, since time.Time) (float64, error)
	// TokensSince sums raw (prompt+completion) tokens since a time.
	TokensSince(ctx context.Context, since time.Time) (int64, error)

	AddPausedTask(ctx context.Context, task domain.QuotaPausedTask) error
	ListPausedTasks(ctx context.Context) ([]domain.QuotaPausedTask, error)
	ClearPausedTasks(ctx context.Context) error
	RemovePausedTask(ctx context.Context, taskID uuid.UUID) error
}
