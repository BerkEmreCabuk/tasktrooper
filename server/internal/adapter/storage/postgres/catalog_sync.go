package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func NewCatalogSyncStore(pool *DB) *CatalogSyncStore {
	return &CatalogSyncStore{pool: pool}
}

type CatalogSyncStore struct {
	pool *DB
}

func (s *CatalogSyncStore) GetCatalogSyncState(ctx context.Context) (domain.CatalogSyncState, error) {
	var out domain.CatalogSyncState
	var summaryJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT repo_ref, last_sync_at, last_error, last_summary, pending_count, updated_at
		FROM catalog_sync_state WHERE id = 1
	`).Scan(&out.RepoRef, &out.LastSyncAt, &out.LastError, &summaryJSON, &out.PendingCount, &out.UpdatedAt)
	if err != nil {
		return domain.CatalogSyncState{}, fmt.Errorf("get catalog sync state: %w", err)
	}
	if len(summaryJSON) > 0 {
		var summary domain.CatalogSyncResult
		if err := json.Unmarshal(summaryJSON, &summary); err == nil {
			out.LastSummary = &summary
		}
	}
	return out, nil
}

func (s *CatalogSyncStore) SaveCatalogSyncState(ctx context.Context, state domain.CatalogSyncState) error {
	summaryJSON := []byte{}
	if state.LastSummary != nil {
		b, err := json.Marshal(state.LastSummary)
		if err != nil {
			return fmt.Errorf("marshal catalog sync summary: %w", err)
		}
		summaryJSON = b
	}
	if state.LastSyncAt.IsZero() {
		state.LastSyncAt = time.Now()
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE catalog_sync_state
		SET repo_ref=$1, last_sync_at=$2, last_error=$3, last_summary=$4, pending_count=$5, updated_at=now()
		WHERE id=1
	`, state.RepoRef, state.LastSyncAt, state.LastError, summaryJSON, state.PendingCount)
	if err != nil {
		return fmt.Errorf("save catalog sync state: %w", err)
	}
	return nil
}

// AppendCatalogPending records an upstream change the sync could not apply. It
// is idempotent per (agent, kind, name, action, reason): a sync that fails the
// same way twice must not grow the ledger forever — that record would be the
// same fact, re-reported.
func (s *CatalogSyncStore) AppendCatalogPending(ctx context.Context, pending domain.CatalogPending) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO catalog_pending (agent_slug, agent_name, kind, name, action, reason)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE NOT EXISTS (
			SELECT 1 FROM catalog_pending
			WHERE agent_slug = $1 AND kind = $3 AND name = $4 AND action = $5 AND reason = $6
		)
	`, pending.AgentSlug, pending.AgentName, pending.Kind, pending.Name, pending.Action, pending.Reason)
	if err != nil {
		return fmt.Errorf("append catalog pending: %w", err)
	}
	return nil
}

func (s *CatalogSyncStore) ListCatalogPending(ctx context.Context) ([]domain.CatalogPending, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_slug, agent_name, kind, name, action, reason, created_at
		FROM catalog_pending ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list catalog pending: %w", err)
	}
	defer rows.Close()
	var out []domain.CatalogPending
	for rows.Next() {
		var p domain.CatalogPending
		if err := rows.Scan(&p.ID, &p.AgentSlug, &p.AgentName, &p.Kind, &p.Name, &p.Action, &p.Reason, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *CatalogSyncStore) DeleteCatalogPending(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM catalog_pending WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete catalog pending: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pending change not found")
	}
	return nil
}
