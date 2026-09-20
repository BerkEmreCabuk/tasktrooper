package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type CatalogVersionStore struct {
	pool *DB
}

func NewCatalogVersionStore(pool *DB) *CatalogVersionStore {
	return &CatalogVersionStore{pool: pool}
}

const catalogVersionColumns = "id, agent_id, target_kind, target_id, version, action, name, description, category, tags, content, priority, enabled, source, reason, reflection_id, created_at"

// AppendVersion computes the next version number in the same statement as the
// insert, so two concurrent writers cannot land on the same number — the
// unique (target_kind, target_id, version) index turns the loser into an
// error instead of a silently overwritten history entry.
func (s *CatalogVersionStore) AppendVersion(ctx context.Context, v domain.CatalogVersion) (domain.CatalogVersion, error) {
	tags := v.Tags
	if tags == nil {
		tags = []string{}
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO catalog_versions
			(agent_id, target_kind, target_id, version, action, name, description, category, tags, content, priority, enabled, source, reason, reflection_id)
		VALUES ($1, $2, $3,
			(SELECT COALESCE(MAX(version), 0) + 1 FROM catalog_versions WHERE target_kind = $2 AND target_id = $3),
			$4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+catalogVersionColumns,
		v.AgentID, v.TargetKind, v.TargetID, v.Action, v.Name, v.Description, v.Category,
		tags, v.Content, v.Priority, v.Enabled, v.Source, v.Reason, v.ReflectionID)
	out, err := scanCatalogVersion(row)
	if err != nil {
		return domain.CatalogVersion{}, fmt.Errorf("append catalog version: %w", err)
	}
	return out, nil
}

func (s *CatalogVersionStore) ListVersions(ctx context.Context, targetKind string, targetID uuid.UUID, limit int) ([]domain.CatalogVersion, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+catalogVersionColumns+`
		FROM catalog_versions
		WHERE target_kind = $1 AND target_id = $2
		ORDER BY version DESC
		LIMIT $3
	`, targetKind, targetID, limit)
	if err != nil {
		return nil, fmt.Errorf("list catalog versions: %w", err)
	}
	defer rows.Close()
	var out []domain.CatalogVersion
	for rows.Next() {
		v, err := scanCatalogVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *CatalogVersionStore) GetVersion(ctx context.Context, targetKind string, targetID uuid.UUID, version int) (domain.CatalogVersion, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+catalogVersionColumns+`
		FROM catalog_versions
		WHERE target_kind = $1 AND target_id = $2 AND version = $3
	`, targetKind, targetID, version)
	out, err := scanCatalogVersion(row)
	if err != nil {
		return domain.CatalogVersion{}, fmt.Errorf("get catalog version: %w", err)
	}
	return out, nil
}

func scanCatalogVersion(row interface{ Scan(dest ...any) error }) (domain.CatalogVersion, error) {
	var v domain.CatalogVersion
	err := row.Scan(&v.ID, &v.AgentID, &v.TargetKind, &v.TargetID, &v.Version, &v.Action,
		&v.Name, &v.Description, &v.Category, &v.Tags, &v.Content, &v.Priority, &v.Enabled,
		&v.Source, &v.Reason, &v.ReflectionID, &v.CreatedAt)
	if err != nil {
		return domain.CatalogVersion{}, err
	}
	return v, nil
}
