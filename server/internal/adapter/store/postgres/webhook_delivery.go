package postgres

import (
	"context"
	"fmt"
	"time"
)

// WebhookDeliveryStore is the durable half of "have we already handled this
// GitHub delivery".
//
// It replaces a map on repository.Service. That map was correct for exactly as
// long as one process handled every delivery: GitHub retries a delivery it did
// not get a 2xx for, and it retries to whichever pod the load balancer picks,
// so a retry landing on a second replica found an empty map and re-ran the
// whole pass — a second reindex over the same push, a second resolve of the
// same workflow run.
type WebhookDeliveryStore struct {
	pool *DB
}

func NewWebhookDeliveryStore(pool *DB) *WebhookDeliveryStore {
	return &WebhookDeliveryStore{pool: pool}
}

// MarkSeen records the delivery and reports whether THIS caller was the first
// to record it.
//
// The insert IS the check. Asking "have we seen it?" and then writing "we have
// now" leaves a window that two pods handling the same retry both pass through;
// a single INSERT ... ON CONFLICT DO NOTHING has no window, because the primary
// key does the deciding inside one statement. GitHub's delivery id is a uuid it
// keeps stable across retries of the same event, which is the whole reason this
// works without hashing any content.
//
// An error is reported rather than folded into "not seen". Failing open is the
// right default here — a duplicated reindex costs embeddings, a DROPPED
// delivery costs a card that never moves — but it is the caller that knows
// that, so it is the caller that decides.
func (s *WebhookDeliveryStore) MarkSeen(ctx context.Context, deliveryID string, retain time.Duration) (bool, error) {
	if deliveryID == "" {
		// No id to dedupe on. GitHub always sends one; a delivery without it
		// did not come from GitHub, and treating it as fresh is what the
		// in-memory version did too.
		return true, nil
	}
	var inserted bool
	err := s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO github_webhook_deliveries (delivery_id)
			VALUES ($1)
			ON CONFLICT (tenant_id, delivery_id) DO NOTHING
			RETURNING delivery_id
		)
		SELECT EXISTS (SELECT 1 FROM ins)
	`, deliveryID).Scan(&inserted)
	if err != nil {
		return false, fmt.Errorf("mark webhook delivery seen: %w", err)
	}
	if inserted && retain > 0 {
		s.prune(ctx, retain)
	}
	return inserted, nil
}

// prune keeps the ledger from growing forever.
//
// It runs on the write path because this repository has no scheduler that is
// not a sweeper, and a sweeper for a table nothing ever reads back would be a
// query a minute for nothing. An index-bounded DELETE on the rare first sight
// of a delivery costs less than the row it just wrote.
//
// Errors go nowhere on purpose: the delivery is already recorded, and a ledger
// that is a few hours longer than intended is not a problem anybody has.
func (s *WebhookDeliveryStore) prune(ctx context.Context, retain time.Duration) {
	_, _ = s.pool.Exec(ctx, `
		DELETE FROM github_webhook_deliveries WHERE seen_at < now() - $1::interval
	`, retain.String())
}
