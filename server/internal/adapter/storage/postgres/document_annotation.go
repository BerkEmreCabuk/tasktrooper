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

type TaskDocumentAnnotationStore struct {
	pool *DB
}

func NewTaskDocumentAnnotationStore(pool *DB) *TaskDocumentAnnotationStore {
	return &TaskDocumentAnnotationStore{pool: pool}
}

const annotationColumns = `id, task_id, document_id, quote, prefix, suffix, body, status, reply,
	created_by_type, created_at, updated_at, submitted_at, resolved_at`

func scanAnnotation(row pgx.Row) (domain.TaskDocumentAnnotation, error) {
	var a domain.TaskDocumentAnnotation
	err := row.Scan(
		&a.ID, &a.TaskID, &a.DocumentID, &a.Quote, &a.Prefix, &a.Suffix, &a.Body, &a.Status, &a.Reply,
		&a.CreatedByType, &a.CreatedAt, &a.UpdatedAt, &a.SubmittedAt, &a.ResolvedAt,
	)
	return a, err
}

func (s *TaskDocumentAnnotationStore) Create(ctx context.Context, a domain.TaskDocumentAnnotation) (domain.TaskDocumentAnnotation, error) {
	status := a.Status
	if status == "" {
		status = domain.AnnotationStatusOpen
	}
	createdBy := a.CreatedByType
	if createdBy == "" {
		createdBy = "user"
	}
	created, err := scanAnnotation(s.pool.QueryRow(ctx, `
		INSERT INTO task_document_annotations (task_id, document_id, quote, prefix, suffix, body, status, created_by_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+annotationColumns,
		a.TaskID, a.DocumentID, a.Quote, a.Prefix, a.Suffix, a.Body, status, createdBy))
	if err != nil {
		return domain.TaskDocumentAnnotation{}, fmt.Errorf("create annotation: %w", err)
	}
	return created, nil
}

func (s *TaskDocumentAnnotationStore) Get(ctx context.Context, taskID, id uuid.UUID) (domain.TaskDocumentAnnotation, error) {
	a, err := scanAnnotation(s.pool.QueryRow(ctx, `
		SELECT `+annotationColumns+` FROM task_document_annotations WHERE id = $1 AND task_id = $2
	`, id, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskDocumentAnnotation{}, fmt.Errorf("%w: %s", domain.ErrAnnotationNotFound, id)
	}
	if err != nil {
		return domain.TaskDocumentAnnotation{}, fmt.Errorf("get annotation: %w", err)
	}
	return a, nil
}

func (s *TaskDocumentAnnotationStore) ListByTask(ctx context.Context, taskID uuid.UUID, documentID *uuid.UUID) ([]domain.TaskDocumentAnnotation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+annotationColumns+` FROM task_document_annotations
		WHERE task_id = $1 AND ($2::uuid IS NULL OR document_id = $2)
		ORDER BY created_at ASC, id ASC
	`, taskID, documentID)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	defer rows.Close()
	out := []domain.TaskDocumentAnnotation{}
	for rows.Next() {
		a, err := scanAnnotation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *TaskDocumentAnnotationStore) Update(ctx context.Context, a domain.TaskDocumentAnnotation) (domain.TaskDocumentAnnotation, error) {
	updated, err := scanAnnotation(s.pool.QueryRow(ctx, `
		UPDATE task_document_annotations
		SET body = $3, status = $4, reply = $5, submitted_at = $6, resolved_at = $7, updated_at = now()
		WHERE id = $1 AND task_id = $2
		RETURNING `+annotationColumns,
		a.ID, a.TaskID, a.Body, a.Status, a.Reply, a.SubmittedAt, a.ResolvedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskDocumentAnnotation{}, fmt.Errorf("%w: %s", domain.ErrAnnotationNotFound, a.ID)
	}
	if err != nil {
		return domain.TaskDocumentAnnotation{}, fmt.Errorf("update annotation: %w", err)
	}
	return updated, nil
}

func (s *TaskDocumentAnnotationStore) Delete(ctx context.Context, taskID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM task_document_annotations WHERE id = $1 AND task_id = $2`, id, taskID)
	if err != nil {
		return fmt.Errorf("delete annotation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", domain.ErrAnnotationNotFound, id)
	}
	return nil
}

func (s *TaskDocumentAnnotationStore) MarkSubmitted(ctx context.Context, taskID uuid.UUID, ids []uuid.UUID, at time.Time) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE task_document_annotations
		SET status = 'submitted', submitted_at = $3, updated_at = now()
		WHERE task_id = $1 AND id = ANY($2) AND status = 'open'
		RETURNING id
	`, taskID, ids, at)
	if err != nil {
		return nil, fmt.Errorf("submit annotations: %w", err)
	}
	defer rows.Close()
	var moved []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		moved = append(moved, id)
	}
	return moved, rows.Err()
}
