package postgres

import (
	"context"
)

type LiveActivityTokenStore struct {
	pool *DB
}

func NewLiveActivityTokenStore(pool *DB) *LiveActivityTokenStore {
	return &LiveActivityTokenStore{pool: pool}
}

func (s *LiveActivityTokenStore) UpsertStartToken(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO live_activity_tokens (kind, token)
		VALUES ('start', $1)
		ON CONFLICT (tenant_id, token) DO UPDATE SET kind = 'start', updated_at = now()`, token)
	return err
}

func (s *LiveActivityTokenStore) UpsertUpdateToken(ctx context.Context, token, taskID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO live_activity_tokens (kind, token, task_id)
		VALUES ('update', $1, $2)
		ON CONFLICT (tenant_id, token) DO UPDATE SET kind = 'update', task_id = EXCLUDED.task_id, updated_at = now()`,
		token, taskID)
	return err
}

func (s *LiveActivityTokenStore) Delete(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM live_activity_tokens WHERE token = $1`, token)
	return err
}

func (s *LiveActivityTokenStore) StartTokens(ctx context.Context) ([]string, error) {
	return s.tokens(ctx, `SELECT token FROM live_activity_tokens WHERE kind = 'start'`)
}

func (s *LiveActivityTokenStore) UpdateTokensForTask(ctx context.Context, taskID string) ([]string, error) {
	return s.tokens(ctx,
		`SELECT token FROM live_activity_tokens WHERE kind = 'update' AND task_id = $1`, taskID)
}

func (s *LiveActivityTokenStore) tokens(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}
