package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskColumnSpanStore struct {
	pool *DB
}

func NewTaskColumnSpanStore(pool *DB) *TaskColumnSpanStore {
	return &TaskColumnSpanStore{pool: pool}
}

var _ port.TaskColumnSpanStore = (*TaskColumnSpanStore)(nil)

// RecordMove is one statement so the close and the open cannot interleave with
// a concurrent move. The `open` CTE is evaluated against the pre-update
// snapshot, which is what makes the same-column case a clean no-op: nothing is
// closed and nothing is inserted.
func (s *TaskColumnSpanStore) RecordMove(ctx context.Context, repositoryID, taskID uuid.UUID, toColumn string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		WITH open AS (
			SELECT id, board_column FROM task_column_spans
			WHERE task_id = $2 AND left_at IS NULL
			ORDER BY entered_at DESC LIMIT 1
		), closed AS (
			UPDATE task_column_spans s
			SET left_at = $4,
			    duration_seconds = GREATEST(0, EXTRACT(EPOCH FROM ($4 - s.entered_at))::int)
			FROM open
			WHERE s.id = open.id AND open.board_column <> $3
			RETURNING s.id
		)
		INSERT INTO task_column_spans (task_id, repository_id, board_column, entered_at, visit_no)
		SELECT $2, $1, $3, $4,
		       COALESCE((SELECT MAX(visit_no) FROM task_column_spans
		                 WHERE task_id = $2 AND board_column = $3), 0) + 1
		WHERE NOT EXISTS (SELECT 1 FROM open WHERE open.board_column = $3)
	`, repositoryID, taskID, toColumn, at)
	if err != nil {
		return fmt.Errorf("record column span: %w", err)
	}
	return nil
}

func (s *TaskColumnSpanStore) AttachAgent(ctx context.Context, taskID, agentID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE task_column_spans SET agent_id = $2
		WHERE id = (
			SELECT id FROM task_column_spans
			WHERE task_id = $1 AND left_at IS NULL AND agent_id IS NULL
			ORDER BY entered_at DESC LIMIT 1
		)
	`, taskID, agentID)
	if err != nil {
		return fmt.Errorf("attach span agent: %w", err)
	}
	return nil
}

func (s *TaskColumnSpanStore) SetReviewVerdict(ctx context.Context, taskID uuid.UUID, column, verdict string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE task_column_spans SET review_verdict = $3
		WHERE id = (
			SELECT id FROM task_column_spans
			WHERE task_id = $1 AND board_column = $2 AND left_at IS NULL
			ORDER BY entered_at DESC LIMIT 1
		)
	`, taskID, column, verdict)
	if err != nil {
		return fmt.Errorf("set review verdict: %w", err)
	}
	return nil
}

func (s *TaskColumnSpanStore) OpenSpan(ctx context.Context, taskID uuid.UUID) (domain.TaskColumnSpan, bool, error) {
	var sp domain.TaskColumnSpan
	var verdict *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, task_id, repository_id, board_column, agent_id,
		       entered_at, left_at, duration_seconds, visit_no, review_verdict
		FROM task_column_spans
		WHERE task_id = $1 AND left_at IS NULL
		ORDER BY entered_at DESC LIMIT 1
	`, taskID).Scan(
		&sp.ID, &sp.TaskID, &sp.RepositoryID, &sp.BoardColumn, &sp.AgentID,
		&sp.EnteredAt, &sp.LeftAt, &sp.DurationSeconds, &sp.VisitNo, &verdict,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskColumnSpan{}, false, nil
	}
	if err != nil {
		return domain.TaskColumnSpan{}, false, fmt.Errorf("open span: %w", err)
	}
	if verdict != nil {
		sp.ReviewVerdict = *verdict
	}
	return sp, true, nil
}

func (s *TaskColumnSpanStore) OwnersForTask(ctx context.Context, taskID uuid.UUID) (map[string]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (board_column) board_column, agent_id
		FROM task_column_spans
		WHERE task_id = $1 AND agent_id IS NOT NULL
		ORDER BY board_column, entered_at
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("span owners: %w", err)
	}
	defer rows.Close()
	owners := make(map[string]uuid.UUID)
	for rows.Next() {
		var col string
		var agentID uuid.UUID
		if err := rows.Scan(&col, &agentID); err != nil {
			return nil, err
		}
		owners[col] = agentID
	}
	return owners, rows.Err()
}

func (s *TaskColumnSpanStore) HasVisited(ctx context.Context, taskID uuid.UUID, column string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM task_column_spans WHERE task_id = $1 AND board_column = $2)
	`, taskID, column).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("span visited: %w", err)
	}
	return exists, nil
}

// LatestVerdicts returns one row per column the task has visited, carrying the
// review verdict of the latest visit. DISTINCT ON with the matching ORDER BY
// picks that visit per column in a single index-ordered pass; visit_no is the
// tiebreaker for the (possible) case of two spans stamped at the same instant.
func (s *TaskColumnSpanStore) LatestVerdicts(ctx context.Context, taskID uuid.UUID) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (board_column) board_column, COALESCE(review_verdict, '')
		FROM task_column_spans
		WHERE task_id = $1
		ORDER BY board_column, entered_at DESC, visit_no DESC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("span verdicts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var col, verdict string
		if err := rows.Scan(&col, &verdict); err != nil {
			return nil, err
		}
		out[col] = verdict
	}
	return out, rows.Err()
}

// CleanTaskHours groups by task first: an agent that held one task across two
// columns (ready_for_qa then in_qa) contributes one number, not two, so the
// median the caller computes is over tasks rather than over spans.
func (s *TaskColumnSpanStore) CleanTaskHours(ctx context.Context, agentID uuid.UUID, columns []string, from, to time.Time) ([]float64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT SUM(s.duration_seconds)::float8 / 3600.0
		FROM task_column_spans s
		JOIN board_tasks t ON t.id = s.task_id
		WHERE s.agent_id = $1
		  AND s.board_column = ANY($2)
		  AND s.duration_seconds IS NOT NULL
		  AND t.clean_completion = true
		  AND t.completed_at >= $3
		  AND t.completed_at < $4
		GROUP BY s.task_id
	`, agentID, columns, from, to)
	if err != nil {
		return nil, fmt.Errorf("clean task hours: %w", err)
	}
	defer rows.Close()
	var hours []float64
	for rows.Next() {
		var h float64
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		hours = append(hours, h)
	}
	return hours, rows.Err()
}
