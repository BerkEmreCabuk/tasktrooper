package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type SessionActionStore struct {
	pool *DB
}

func NewSessionActionStore(pool *DB) *SessionActionStore {
	return &SessionActionStore{pool: pool}
}

const sessionActionColumns = `id, session_id, run_id, agent_id, tool_name, verb, entity_kind,
	entity_id, entity_key, title, task_column, priority, repository_id, payload, is_error, created_at`

func (s *SessionActionStore) AppendAction(ctx context.Context, action domain.SessionAction) (domain.SessionAction, error) {
	// jsonb rejects an empty parameter, and a literal SQL null reads back as an
	// unusable payload — normalise both to an empty object, as session_steps does.
	payload := []byte(action.Payload)
	if len(payload) == 0 || string(payload) == "null" {
		payload = []byte("{}")
	}

	var out domain.SessionAction
	err := s.pool.QueryRow(ctx, `
		INSERT INTO session_actions (
			session_id, run_id, agent_id, tool_name, verb, entity_kind,
			entity_id, entity_key, title, task_column, priority, repository_id, payload, is_error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+sessionActionColumns,
		action.SessionID, action.RunID, action.AgentID, action.ToolName, action.Verb,
		string(action.EntityKind), action.EntityID, action.EntityKey, action.Title,
		action.Column, action.Priority, action.RepositoryID, payload, action.IsError,
	).Scan(scanSessionActionDest(&out)...)
	if err != nil {
		return domain.SessionAction{}, fmt.Errorf("append session action: %w", err)
	}
	return out, nil
}

func (s *SessionActionStore) ListActions(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionAction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+sessionActionColumns+`
		FROM session_actions WHERE session_id = $1 ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session actions: %w", err)
	}
	defer rows.Close()

	actions := make([]domain.SessionAction, 0)
	for rows.Next() {
		var a domain.SessionAction
		if err := rows.Scan(scanSessionActionDest(&a)...); err != nil {
			return nil, fmt.Errorf("scan session action: %w", err)
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

func scanSessionActionDest(a *domain.SessionAction) []any {
	return []any{
		&a.ID, &a.SessionID, &a.RunID, &a.AgentID, &a.ToolName, &a.Verb, &a.EntityKind,
		&a.EntityID, &a.EntityKey, &a.Title, &a.Column, &a.Priority, &a.RepositoryID,
		&a.Payload, &a.IsError, &a.CreatedAt,
	}
}
