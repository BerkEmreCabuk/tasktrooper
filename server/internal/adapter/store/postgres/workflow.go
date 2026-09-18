package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type WorkflowStore struct {
	pool *DB
}

func NewWorkflowStore(pool *DB) *WorkflowStore {
	return &WorkflowStore{pool: pool}
}

// ErrTaskTypeNotFound is returned by GetTaskType/UpdateTaskType/DeleteTaskType
// when the key names no row.
var ErrTaskTypeNotFound = errors.New("task type not found")

const taskTypeSelect = `
	SELECT tt.key, tt.label, tt.key_prefix, tt.position, tt.is_default, tt.is_defect,
		tt.assignee_role_id, tt.assignee_mode, tt.behaviours, tt.built_in,
		COALESCE((SELECT COUNT(*) FROM board_tasks bt WHERE bt.task_type = tt.key), 0)
	FROM task_types tt
`

func scanTaskType(row pgx.Row) (domain.TaskTypeDef, error) {
	var t domain.TaskTypeDef
	var behavioursJSON []byte
	var mode string
	var count int64
	if err := row.Scan(&t.Key, &t.Label, &t.KeyPrefix, &t.Position, &t.IsDefault, &t.IsDefect,
		&t.AssigneeRoleID, &mode, &behavioursJSON, &t.BuiltIn, &count); err != nil {
		return domain.TaskTypeDef{}, err
	}
	t.AssigneeMode = domain.AssigneeMode(mode)
	t.TaskCount = int(count)
	_ = json.Unmarshal(behavioursJSON, &t.Behaviours)
	if t.Behaviours == nil {
		t.Behaviours = []domain.BehaviourRef{}
	}
	return t, nil
}

func (s *WorkflowStore) ListTaskTypes(ctx context.Context) ([]domain.TaskTypeDef, error) {
	rows, err := s.pool.Query(ctx, taskTypeSelect+` ORDER BY tt.position`)
	if err != nil {
		return nil, fmt.Errorf("list task types: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskTypeDef
	for rows.Next() {
		t, err := scanTaskType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *WorkflowStore) GetTaskType(ctx context.Context, key domain.TaskType) (domain.TaskTypeDef, error) {
	row := s.pool.QueryRow(ctx, taskTypeSelect+` WHERE tt.key = $1`, string(key))
	t, err := scanTaskType(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskTypeDef{}, fmt.Errorf("get task type %s: %w", key, ErrTaskTypeNotFound)
	}
	if err != nil {
		return domain.TaskTypeDef{}, fmt.Errorf("get task type: %w", err)
	}
	return t, nil
}

func behavioursJSON(b []domain.BehaviourRef) ([]byte, error) {
	if b == nil {
		b = []domain.BehaviourRef{}
	}
	return json.Marshal(b)
}

func (s *WorkflowStore) CreateTaskType(ctx context.Context, def domain.TaskTypeDef, cloneFrom domain.TaskType) (domain.TaskTypeDef, error) {
	behJSON, err := behavioursJSON(def.Behaviours)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO task_types (key, label, key_prefix, position, is_default, is_defect, assignee_role_id, assignee_mode, behaviours, built_in)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, false)
	`, string(def.Key), def.Label, def.KeyPrefix, def.Position, def.IsDefault, def.IsDefect,
		def.AssigneeRoleID, string(def.AssigneeMode), behJSON); err != nil {
		return domain.TaskTypeDef{}, fmt.Errorf("create task type: %w", err)
	}

	if cloneFrom != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions)
			SELECT $1, column_slug, position, on_path, kind, behaviours, instructions
			FROM workflow_stages WHERE task_type = $2
		`, string(def.Key), string(cloneFrom)); err != nil {
			return domain.TaskTypeDef{}, fmt.Errorf("clone workflow stages: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_stage_participants (stage_id, role_id, mode, instructions, position)
			SELECT new_stage.id, old_p.role_id, old_p.mode, old_p.instructions, old_p.position
			FROM workflow_stage_participants old_p
			JOIN workflow_stages old_stage ON old_stage.id = old_p.stage_id AND old_stage.task_type = $2
			JOIN workflow_stages new_stage ON new_stage.task_type = $1 AND new_stage.column_slug = old_stage.column_slug
		`, string(def.Key), string(cloneFrom)); err != nil {
			return domain.TaskTypeDef{}, fmt.Errorf("clone workflow stage participants: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.TaskTypeDef{}, err
	}
	return s.GetTaskType(ctx, def.Key)
}

func (s *WorkflowStore) UpdateTaskType(ctx context.Context, def domain.TaskTypeDef) (domain.TaskTypeDef, error) {
	behJSON, err := behavioursJSON(def.Behaviours)
	if err != nil {
		return domain.TaskTypeDef{}, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE task_types SET
			label = $2, key_prefix = $3, position = $4, is_default = $5, is_defect = $6,
			assignee_role_id = $7, assignee_mode = $8, behaviours = $9, updated_at = now()
		WHERE key = $1
	`, string(def.Key), def.Label, def.KeyPrefix, def.Position, def.IsDefault, def.IsDefect,
		def.AssigneeRoleID, string(def.AssigneeMode), behJSON)
	if err != nil {
		return domain.TaskTypeDef{}, fmt.Errorf("update task type: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.TaskTypeDef{}, fmt.Errorf("update task type %s: %w", def.Key, ErrTaskTypeNotFound)
	}
	return s.GetTaskType(ctx, def.Key)
}

func (s *WorkflowStore) DeleteTaskType(ctx context.Context, key domain.TaskType) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM task_types WHERE key = $1`, string(key))
	if err != nil {
		return fmt.Errorf("delete task type: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete task type %s: %w", key, ErrTaskTypeNotFound)
	}
	return nil
}

const stageSelect = `SELECT id, task_type, column_slug, position, on_path, kind, behaviours, instructions FROM workflow_stages`

func scanStage(row pgx.Row) (domain.WorkflowStage, error) {
	var st domain.WorkflowStage
	var taskType, col, kind string
	var behavioursJSON []byte
	if err := row.Scan(&st.ID, &taskType, &col, &st.Position, &st.OnPath, &kind, &behavioursJSON, &st.Instructions); err != nil {
		return domain.WorkflowStage{}, err
	}
	st.TaskType = domain.TaskType(taskType)
	st.Column = domain.TaskColumn(col)
	st.Kind = domain.StageKind(kind)
	_ = json.Unmarshal(behavioursJSON, &st.Behaviours)
	if st.Behaviours == nil {
		st.Behaviours = []domain.BehaviourRef{}
	}
	return st, nil
}

func (s *WorkflowStore) ListStages(ctx context.Context, taskType domain.TaskType) ([]domain.WorkflowStage, error) {
	rows, err := s.pool.Query(ctx, stageSelect+` WHERE task_type = $1 ORDER BY position`, string(taskType))
	if err != nil {
		return nil, fmt.Errorf("list workflow stages: %w", err)
	}
	defer rows.Close()
	var out []domain.WorkflowStage
	for rows.Next() {
		st, err := scanStage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	participants, err := s.participantsByStage(ctx, taskType)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Participants = participants[out[i].ID]
	}
	return out, nil
}

func (s *WorkflowStore) participantsByStage(ctx context.Context, taskType domain.TaskType) (map[uuid.UUID][]domain.StageParticipant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.stage_id, p.role_id, p.mode, p.instructions, p.position
		FROM workflow_stage_participants p
		JOIN workflow_stages st ON st.id = p.stage_id
		WHERE st.task_type = $1
		ORDER BY p.position
	`, string(taskType))
	if err != nil {
		return nil, fmt.Errorf("list stage participants: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]domain.StageParticipant)
	for rows.Next() {
		var stageID uuid.UUID
		var p domain.StageParticipant
		var mode string
		if err := rows.Scan(&stageID, &p.RoleID, &mode, &p.Instructions, &p.Position); err != nil {
			return nil, err
		}
		p.Mode = domain.ParticipantMode(mode)
		out[stageID] = append(out[stageID], p)
	}
	return out, rows.Err()
}

// ReplaceStages replaces a type's whole stage set (and every participant row,
// cascaded) in one transaction — the UI always sends the complete ordered
// stage list, never a partial patch, so a diff would only add complexity no
// caller needs.
func (s *WorkflowStore) ReplaceStages(ctx context.Context, taskType domain.TaskType, stages []domain.WorkflowStage) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM workflow_stages WHERE task_type = $1`, string(taskType)); err != nil {
		return fmt.Errorf("clear workflow stages: %w", err)
	}
	for _, st := range stages {
		behJSON, err := behavioursJSON(st.Behaviours)
		if err != nil {
			return err
		}
		var stageID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO workflow_stages (task_type, column_slug, position, on_path, kind, behaviours, instructions)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
		`, string(taskType), string(st.Column), st.Position, st.OnPath, string(st.Kind), behJSON, st.Instructions).Scan(&stageID)
		if err != nil {
			return fmt.Errorf("insert workflow stage %s: %w", st.Column, err)
		}
		for _, p := range st.Participants {
			if _, err := tx.Exec(ctx, `
				INSERT INTO workflow_stage_participants (stage_id, role_id, mode, instructions, position)
				VALUES ($1, $2, $3, $4, $5)
			`, stageID, p.RoleID, string(p.Mode), p.Instructions, p.Position); err != nil {
				return fmt.Errorf("insert stage participant: %w", err)
			}
		}
	}
	return tx.Commit(ctx)
}

// ColumnHasBehaviourStages reports whether any stage at this column slug (any
// task type) carries a non-empty behaviours array.
func (s *WorkflowStore) ColumnHasBehaviourStages(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM workflow_stages
			WHERE column_slug = $1 AND jsonb_array_length(behaviours) > 0
		)
	`, slug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check column workflow stages: %w", err)
	}
	return exists, nil
}

// LoadAll reads every task type with its full stage set in a handful of
// queries — what application/workflow.Service calls at boot and after every
// write to rebuild its in-memory snapshot. The engine's hot path never runs
// this; it reads the snapshot LoadAll produced.
func (s *WorkflowStore) LoadAll(ctx context.Context) ([]domain.Workflow, error) {
	types, err := s.ListTaskTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Workflow, 0, len(types))
	for _, t := range types {
		stages, err := s.ListStages(ctx, t.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.Workflow{Type: t, Stages: stages})
	}
	return out, nil
}
