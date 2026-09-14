package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// LLMUsageStore persists per-call token usage and serves aggregates.
type LLMUsageStore interface {
	Record(ctx context.Context, rec domain.LLMUsageRecord) error
	Summary(ctx context.Context, days int) (domain.LLMUsageSummary, error)
}
