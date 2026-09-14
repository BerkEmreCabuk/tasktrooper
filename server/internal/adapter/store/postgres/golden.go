package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type GoldenStore struct {
	pool *DB
}

func NewGoldenStore(pool *DB) *GoldenStore {
	return &GoldenStore{pool: pool}
}

const goldenColumns = "id, agent_id, name, prompt, expected, enabled, created_at, updated_at"

func (s *GoldenStore) CreateTask(ctx context.Context, t domain.GoldenTask) (domain.GoldenTask, error) {
	expected, err := json.Marshal(orEmptyStrings(t.Expected))
	if err != nil {
		return domain.GoldenTask{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_golden_tasks (agent_id, name, prompt, expected, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+goldenColumns,
		t.AgentID, t.Name, t.Prompt, expected, t.Enabled)
	out, err := scanGoldenTask(row)
	if err != nil {
		return domain.GoldenTask{}, fmt.Errorf("create golden task: %w", err)
	}
	return out, nil
}

func (s *GoldenStore) UpdateTask(ctx context.Context, t domain.GoldenTask) (domain.GoldenTask, error) {
	expected, err := json.Marshal(orEmptyStrings(t.Expected))
	if err != nil {
		return domain.GoldenTask{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE agent_golden_tasks SET name=$2, prompt=$3, expected=$4, enabled=$5, updated_at=now()
		WHERE id=$1
		RETURNING `+goldenColumns,
		t.ID, t.Name, t.Prompt, expected, t.Enabled)
	out, err := scanGoldenTask(row)
	if err != nil {
		return domain.GoldenTask{}, fmt.Errorf("update golden task: %w", err)
	}
	return out, nil
}

func (s *GoldenStore) DeleteTask(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM agent_golden_tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete golden task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("golden task not found")
	}
	return nil
}

func (s *GoldenStore) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.GoldenTask, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+goldenColumns+` FROM agent_golden_tasks WHERE agent_id = $1 ORDER BY created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list golden tasks: %w", err)
	}
	defer rows.Close()
	var out []domain.GoldenTask
	for rows.Next() {
		t, err := scanGoldenTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *GoldenStore) SaveResult(ctx context.Context, r domain.GoldenResult) (domain.GoldenResult, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_golden_results (golden_id, agent_id, reflection_id, passed, detail)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, golden_id, agent_id, reflection_id, passed, detail, created_at
	`, r.GoldenID, r.AgentID, r.ReflectionID, r.Passed, r.Detail)
	var out domain.GoldenResult
	if err := row.Scan(&out.ID, &out.GoldenID, &out.AgentID, &out.ReflectionID, &out.Passed, &out.Detail, &out.CreatedAt); err != nil {
		return domain.GoldenResult{}, fmt.Errorf("save golden result: %w", err)
	}
	return out, nil
}

func (s *GoldenStore) ListResults(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.GoldenResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, golden_id, agent_id, reflection_id, passed, detail, created_at
		FROM agent_golden_results WHERE agent_id = $1
		ORDER BY created_at DESC LIMIT $2
	`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list golden results: %w", err)
	}
	defer rows.Close()
	var out []domain.GoldenResult
	for rows.Next() {
		var r domain.GoldenResult
		if err := rows.Scan(&r.ID, &r.GoldenID, &r.AgentID, &r.ReflectionID, &r.Passed, &r.Detail, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanGoldenTask(row pgx.Row) (domain.GoldenTask, error) {
	var t domain.GoldenTask
	var expected []byte
	if err := row.Scan(&t.ID, &t.AgentID, &t.Name, &t.Prompt, &expected, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return domain.GoldenTask{}, err
	}
	_ = json.Unmarshal(expected, &t.Expected)
	return t, nil
}

func orEmptyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
