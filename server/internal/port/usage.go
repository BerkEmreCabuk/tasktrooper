package port

import (
	"context"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// LLMUsageStore persists per-call token usage and serves aggregates.
// Summary buckets days using loc, so "today" in the dashboard's timezone
// matches what the browser sent, not the server's own local time.
type LLMUsageStore interface {
	Record(ctx context.Context, rec domain.LLMUsageRecord) error
	Summary(ctx context.Context, since time.Time, loc *time.Location) (domain.LLMUsageSummary, error)
}
