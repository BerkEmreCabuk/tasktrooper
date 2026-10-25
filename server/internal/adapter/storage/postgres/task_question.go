package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type TaskQuestionStore struct {
	pool *DB
}

func NewTaskQuestionStore(pool *DB) *TaskQuestionStore {
	return &TaskQuestionStore{pool: pool}
}

const questionColumns = `id, task_id, question_key, prompt, kind, blocking, recommended_answer, status, answer,
	answered_at, submitted_at, created_at, updated_at`

func scanQuestion(row pgx.Row) (domain.TaskQuestion, error) {
	var q domain.TaskQuestion
	err := row.Scan(
		&q.ID, &q.TaskID, &q.Key, &q.Prompt, &q.Kind, &q.Blocking, &q.RecommendedAnswer, &q.Status, &q.Answer,
		&q.AnsweredAt, &q.SubmittedAt, &q.CreatedAt, &q.UpdatedAt,
	)
	return q, err
}

// Create assigns question_key in the same transaction as the insert, a
// pg_advisory_xact_lock on taskID serializing it against any other create on
// the same task — including the very first one, where there is no existing
// row to lock instead. record_open_questions calls this once per question in
// its add list, so the lock is held only briefly and never nested.
func (s *TaskQuestionStore) Create(ctx context.Context, q domain.TaskQuestion) (domain.TaskQuestion, error) {
	var created domain.TaskQuestion
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, q.TaskID.String()); err != nil {
			return fmt.Errorf("lock task questions: %w", err)
		}
		var maxNum int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(substring(question_key FROM 2)::int), 0)
			FROM task_questions WHERE task_id = $1
		`, q.TaskID).Scan(&maxNum); err != nil {
			return fmt.Errorf("compute next question key: %w", err)
		}
		key := fmt.Sprintf("Q%d", maxNum+1)
		status := q.Status
		if status == "" {
			status = domain.QuestionStatusOpen
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO task_questions (task_id, question_key, prompt, kind, blocking, recommended_answer, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING `+questionColumns,
			q.TaskID, key, q.Prompt, q.Kind, q.Blocking, q.RecommendedAnswer, status)
		var err error
		created, err = scanQuestion(row)
		if err != nil {
			return fmt.Errorf("create task question: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.TaskQuestion{}, err
	}
	return created, nil
}

func (s *TaskQuestionStore) Get(ctx context.Context, taskID, id uuid.UUID) (domain.TaskQuestion, error) {
	q, err := scanQuestion(s.pool.QueryRow(ctx, `
		SELECT `+questionColumns+` FROM task_questions WHERE id = $1 AND task_id = $2
	`, id, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskQuestion{}, fmt.Errorf("%w: %s", domain.ErrQuestionNotFound, id)
	}
	if err != nil {
		return domain.TaskQuestion{}, fmt.Errorf("get task question: %w", err)
	}
	return q, nil
}

func (s *TaskQuestionStore) GetByKey(ctx context.Context, taskID uuid.UUID, key string) (domain.TaskQuestion, error) {
	q, err := scanQuestion(s.pool.QueryRow(ctx, `
		SELECT `+questionColumns+` FROM task_questions WHERE task_id = $1 AND question_key = $2
	`, taskID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskQuestion{}, fmt.Errorf("%w: %s", domain.ErrQuestionNotFound, key)
	}
	if err != nil {
		return domain.TaskQuestion{}, fmt.Errorf("get task question by key: %w", err)
	}
	return q, nil
}

// ListByTask orders by question_key's numeric suffix (Q1, Q2, … Q10) rather
// than lexically, which would put Q10 before Q2.
func (s *TaskQuestionStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskQuestion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+questionColumns+` FROM task_questions
		WHERE task_id = $1
		ORDER BY substring(question_key FROM 2)::int, question_key
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task questions: %w", err)
	}
	defer rows.Close()
	out := []domain.TaskQuestion{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *TaskQuestionStore) Update(ctx context.Context, q domain.TaskQuestion) (domain.TaskQuestion, error) {
	updated, err := scanQuestion(s.pool.QueryRow(ctx, `
		UPDATE task_questions
		SET prompt = $3, kind = $4, blocking = $5, recommended_answer = $6,
		    status = $7, answer = $8, answered_at = $9, submitted_at = $10, updated_at = now()
		WHERE id = $1 AND task_id = $2
		RETURNING `+questionColumns,
		q.ID, q.TaskID, q.Prompt, q.Kind, q.Blocking, q.RecommendedAnswer,
		q.Status, q.Answer, q.AnsweredAt, q.SubmittedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskQuestion{}, fmt.Errorf("%w: %s", domain.ErrQuestionNotFound, q.ID)
	}
	if err != nil {
		return domain.TaskQuestion{}, fmt.Errorf("update task question: %w", err)
	}
	return updated, nil
}

// MarkSubmitted stamps submitted_at on every answered, unsubmitted question
// of the task in one statement — the common half of /questions/submit,
// /annotations/submit and a done approval.
func (s *TaskQuestionStore) MarkSubmitted(ctx context.Context, taskID uuid.UUID, at time.Time) ([]domain.TaskQuestion, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE task_questions
		SET submitted_at = $2, updated_at = now()
		WHERE task_id = $1 AND status = 'answered' AND submitted_at IS NULL
		RETURNING `+questionColumns,
		taskID, at)
	if err != nil {
		return nil, fmt.Errorf("mark task questions submitted: %w", err)
	}
	defer rows.Close()
	out := []domain.TaskQuestion{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
