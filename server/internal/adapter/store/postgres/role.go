package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type RoleStore struct {
	pool *DB
}

func NewRoleStore(pool *DB) *RoleStore {
	return &RoleStore{pool: pool}
}

const roleColumns = "id, key, name, description, required_tools"

func scanRole(row pgx.Row) (domain.AgentRole, error) {
	var r domain.AgentRole
	if err := row.Scan(&r.ID, &r.Key, &r.Name, &r.Description, &r.RequiredTools); err != nil {
		return domain.AgentRole{}, err
	}
	return r, nil
}

// List returns every role with its assignments and purposes filled in — the
// admin page's one round trip. Assignment agent names are joined in for
// display; a role whose assignee agent was deleted keeps no row (ON DELETE
// CASCADE on agent_role_assignments).
func (s *RoleStore) List(ctx context.Context) ([]domain.AgentRole, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+roleColumns+` FROM roles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentRole
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	assignmentsByRole, err := s.assignmentsByRole(ctx)
	if err != nil {
		return nil, err
	}
	purposesByRole, err := s.purposesByRole(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Assignments = assignmentsByRole[out[i].ID]
		out[i].Purposes = purposesByRole[out[i].ID]
	}
	return out, nil
}

func (s *RoleStore) assignmentsByRole(ctx context.Context) (map[uuid.UUID][]domain.RoleAssignment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.role_id, a.agent_id, ag.name, a.areas, a.priority
		FROM agent_role_assignments a
		JOIN agents ag ON ag.id = a.agent_id
		ORDER BY a.priority, a.created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("list role assignments: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]domain.RoleAssignment)
	for rows.Next() {
		var roleID uuid.UUID
		var a domain.RoleAssignment
		if err := rows.Scan(&roleID, &a.AgentID, &a.AgentName, &a.Areas, &a.Priority); err != nil {
			return nil, err
		}
		out[roleID] = append(out[roleID], a)
	}
	return out, rows.Err()
}

func (s *RoleStore) purposesByRole(ctx context.Context) (map[uuid.UUID][]domain.RolePurposeKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT purpose, role_id FROM role_purposes WHERE role_id IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list role purposes: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]domain.RolePurposeKey)
	for rows.Next() {
		var purpose string
		var roleID uuid.UUID
		if err := rows.Scan(&purpose, &roleID); err != nil {
			return nil, err
		}
		out[roleID] = append(out[roleID], domain.RolePurposeKey(purpose))
	}
	return out, rows.Err()
}

func (s *RoleStore) Get(ctx context.Context, id uuid.UUID) (domain.AgentRole, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+roleColumns+` FROM roles WHERE id = $1`, id)
	r, err := scanRole(row)
	if err != nil {
		return domain.AgentRole{}, fmt.Errorf("get role: %w", err)
	}
	assignmentsByRole, err := s.assignmentsByRole(ctx)
	if err != nil {
		return domain.AgentRole{}, err
	}
	purposesByRole, err := s.purposesByRole(ctx)
	if err != nil {
		return domain.AgentRole{}, err
	}
	r.Assignments = assignmentsByRole[r.ID]
	r.Purposes = purposesByRole[r.ID]
	return r, nil
}

func (s *RoleStore) Create(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO roles (key, name, description, required_tools)
		VALUES ($1, $2, $3, $4)
		RETURNING `+roleColumns,
		role.Key, role.Name, role.Description, role.RequiredTools)
	r, err := scanRole(row)
	if err != nil {
		return domain.AgentRole{}, fmt.Errorf("create role: %w", err)
	}
	return r, nil
}

// Update never touches key: it is immutable once created (see role_agent.go
// callers that key agent-tool checks off it).
func (s *RoleStore) Update(ctx context.Context, role domain.AgentRole) (domain.AgentRole, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE roles SET name = $2, description = $3, required_tools = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+roleColumns,
		role.ID, role.Name, role.Description, role.RequiredTools)
	r, err := scanRole(row)
	if err != nil {
		return domain.AgentRole{}, fmt.Errorf("update role: %w", err)
	}
	return r, nil
}

func (s *RoleStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	return nil
}

func (s *RoleStore) SetAssignments(ctx context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM agent_role_assignments WHERE role_id = $1`, roleID); err != nil {
		return fmt.Errorf("clear role assignments: %w", err)
	}
	for _, a := range assignments {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_role_assignments (role_id, agent_id, areas, priority)
			VALUES ($1, $2, $3, $4)
		`, roleID, a.AgentID, a.Areas, a.Priority); err != nil {
			return fmt.Errorf("insert role assignment: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *RoleStore) ListAssignmentsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.AgentRole, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.key, r.name, r.description, r.required_tools, a.areas, a.priority
		FROM agent_role_assignments a
		JOIN roles r ON r.id = a.role_id
		WHERE a.agent_id = $1
		ORDER BY r.name
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list assignments by agent: %w", err)
	}
	defer rows.Close()
	var out []domain.AgentRole
	for rows.Next() {
		var r domain.AgentRole
		var areas []string
		var priority int
		if err := rows.Scan(&r.ID, &r.Key, &r.Name, &r.Description, &r.RequiredTools, &areas, &priority); err != nil {
			return nil, err
		}
		r.Assignments = []domain.RoleAssignment{{AgentID: agentID, Areas: areas, Priority: priority}}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *RoleStore) SetAgentRoles(ctx context.Context, agentID uuid.UUID, roles []domain.AgentRoleMembership) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM agent_role_assignments WHERE agent_id = $1`, agentID); err != nil {
		return fmt.Errorf("clear agent roles: %w", err)
	}
	for _, m := range roles {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_role_assignments (role_id, agent_id, areas, priority)
			VALUES ($1, $2, $3, 0)
		`, m.RoleID, agentID, m.Areas); err != nil {
			return fmt.Errorf("insert agent role: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *RoleStore) ListPurposes(ctx context.Context) ([]domain.RolePurpose, error) {
	rows, err := s.pool.Query(ctx, `SELECT purpose, role_id FROM role_purposes ORDER BY purpose`)
	if err != nil {
		return nil, fmt.Errorf("list purposes: %w", err)
	}
	defer rows.Close()
	var out []domain.RolePurpose
	for rows.Next() {
		var p domain.RolePurpose
		var purpose string
		if err := rows.Scan(&purpose, &p.RoleID); err != nil {
			return nil, err
		}
		p.Purpose = domain.RolePurposeKey(purpose)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *RoleStore) SetPurpose(ctx context.Context, purpose domain.RolePurposeKey, roleID *uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO role_purposes (purpose, role_id) VALUES ($1, $2)
		ON CONFLICT (purpose) DO UPDATE SET role_id = EXCLUDED.role_id
	`, string(purpose), roleID)
	if err != nil {
		return fmt.Errorf("set purpose %s: %w", purpose, err)
	}
	return nil
}
