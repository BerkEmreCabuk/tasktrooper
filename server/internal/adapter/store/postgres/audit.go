package postgres

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AuditStore struct {
	pool *DB
}

func NewAuditStore(pool *DB) *AuditStore {
	return &AuditStore{pool: pool}
}

func (a *AuditStore) Log(ctx context.Context, entry domain.AuditEntry) error {
	_, err := a.pool.Exec(ctx, `
		INSERT INTO audit_logs (request_id, api_key_name, tool_name, arguments, result_preview, duration_ms, is_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, entry.RequestID, entry.APIKeyName, entry.ToolName, entry.Arguments, entry.ResultPreview, entry.DurationMs, entry.IsError)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func (a *AuditStore) List(ctx context.Context, limit int) ([]domain.AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := a.pool.Query(ctx, `
		SELECT id, request_id, api_key_name, tool_name, arguments, result_preview, duration_ms, is_error, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()

	var entries []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		if err := rows.Scan(&e.ID, &e.RequestID, &e.APIKeyName, &e.ToolName, &e.Arguments, &e.ResultPreview, &e.DurationMs, &e.IsError, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
