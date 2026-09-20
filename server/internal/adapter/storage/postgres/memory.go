package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type MemoryStore struct {
	pool *DB
}

func NewMemoryStore(pool *DB) *MemoryStore {
	return &MemoryStore{pool: pool}
}

const memoryColumns = "id, agent_id, repository_id, content, category, embedding, source, created_at, updated_at"

func (s *MemoryStore) Create(ctx context.Context, m domain.AgentMemory) (domain.AgentMemory, error) {
	var embJSON []byte
	if len(m.Embedding) > 0 {
		var err error
		embJSON, err = json.Marshal(m.Embedding)
		if err != nil {
			return domain.AgentMemory{}, err
		}
	}
	var agentID *uuid.UUID
	if m.AgentID != uuid.Nil {
		agentID = &m.AgentID
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_memories (agent_id, repository_id, content, category, embedding, source)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+memoryColumns,
		agentID, m.RepositoryID, m.Content, m.Category, embJSON, m.Source)
	out, err := scanMemory(row)
	if err != nil {
		return domain.AgentMemory{}, fmt.Errorf("create memory: %w", err)
	}
	return out, nil
}

func (s *MemoryStore) Get(ctx context.Context, id uuid.UUID) (domain.AgentMemory, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+memoryColumns+` FROM agent_memories WHERE id = $1`, id)
	out, err := scanMemory(row)
	if err != nil {
		return domain.AgentMemory{}, fmt.Errorf("get memory: %w", err)
	}
	return out, nil
}

func (s *MemoryStore) List(ctx context.Context, q domain.MemoryQuery) ([]domain.AgentMemory, error) {
	sql, args := buildMemoryListQuery(q)
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

// buildMemoryListQuery turns a MemoryQuery into SQL. It is separate from the
// pool call so the bucket logic — the part that decides what an agent is
// allowed to recall — can be tested without a database.
func buildMemoryListQuery(q domain.MemoryQuery) (string, []any) {
	var (
		args   []any
		clause []string
	)
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	switch {
	case q.Owner == domain.MemoryOwnerTeam || q.AgentID == uuid.Nil:
		clause = append(clause, "agent_id IS NULL")
	case q.Owner == domain.MemoryOwnerAgent:
		clause = append(clause, "agent_id = "+next(q.AgentID))
	default:
		clause = append(clause, "(agent_id = "+next(q.AgentID)+" OR agent_id IS NULL)")
	}

	switch {
	case q.Repo == domain.MemoryRepoScopeAny:
		// no repository constraint
	case q.Repo == domain.MemoryRepoScopeGlobal || q.RepositoryID == nil:
		// Without a repository in play, project memories stay out of reach:
		// another repo's lessons are noise at best and wrong at worst.
		clause = append(clause, "repository_id IS NULL")
	case q.Repo == domain.MemoryRepoScopeProject:
		clause = append(clause, "repository_id = "+next(*q.RepositoryID))
	default:
		clause = append(clause, "(repository_id IS NULL OR repository_id = "+next(*q.RepositoryID)+")")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	sql := `SELECT ` + memoryColumns + ` FROM agent_memories WHERE ` +
		strings.Join(clause, " AND ") +
		` ORDER BY created_at DESC LIMIT ` + next(limit)
	return sql, args
}

func (s *MemoryStore) Update(ctx context.Context, m domain.AgentMemory) (domain.AgentMemory, error) {
	var embJSON []byte
	if len(m.Embedding) > 0 {
		var err error
		embJSON, err = json.Marshal(m.Embedding)
		if err != nil {
			return domain.AgentMemory{}, err
		}
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE agent_memories
		SET content = $2, category = $3, embedding = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+memoryColumns,
		m.ID, m.Content, m.Category, embJSON)
	out, err := scanMemory(row)
	if err != nil {
		return domain.AgentMemory{}, fmt.Errorf("update memory: %w", err)
	}
	return out, nil
}

func (s *MemoryStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM agent_memories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory not found")
	}
	return nil
}

func (s *MemoryStore) CountInScope(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_memories
		WHERE agent_id = $1
		  AND (($2::uuid IS NULL AND repository_id IS NULL) OR repository_id = $2)
	`, agentID, repositoryID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count memories: %w", err)
	}
	return n, nil
}

func (s *MemoryStore) DeleteOldestInScope(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, n int) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM agent_memories WHERE id IN (
			SELECT id FROM agent_memories
			WHERE agent_id = $1
			  AND (($2::uuid IS NULL AND repository_id IS NULL) OR repository_id = $2)
			ORDER BY created_at ASC LIMIT $3
		)
	`, agentID, repositoryID, n)
	if err != nil {
		return fmt.Errorf("delete oldest memories: %w", err)
	}
	return nil
}

func scanMemory(row pgx.Row) (domain.AgentMemory, error) {
	var m domain.AgentMemory
	var embJSON []byte
	var agentID *uuid.UUID
	if err := row.Scan(&m.ID, &agentID, &m.RepositoryID, &m.Content, &m.Category, &embJSON, &m.Source, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return domain.AgentMemory{}, err
	}
	if agentID != nil {
		m.AgentID = *agentID
	}
	m.Scope = domain.MemoryScopeOf(m.AgentID, m.RepositoryID)
	if len(embJSON) > 0 {
		_ = json.Unmarshal(embJSON, &m.Embedding)
	}
	return m, nil
}

func scanMemories(rows pgx.Rows) ([]domain.AgentMemory, error) {
	var out []domain.AgentMemory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
