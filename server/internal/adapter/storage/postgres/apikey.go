package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type APIKeyStore struct {
	pool *DB
}

func NewAPIKeyStore(pool *DB) *APIKeyStore {
	return &APIKeyStore{pool: pool}
}

func (s *APIKeyStore) Create(ctx context.Context, name, keyHash, keyPrefix string, policy domain.ToolPolicy) (domain.APIKeyRecord, error) {
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return domain.APIKeyRecord{}, err
	}
	var rec domain.APIKeyRecord
	err = s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (name, key_hash, key_prefix, tool_policy)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, key_prefix, tool_policy, created_at
	`, name, keyHash, keyPrefix, policyJSON).Scan(&rec.ID, &rec.Name, &rec.KeyPrefix, &policyJSON, &rec.CreatedAt)
	if err != nil {
		return domain.APIKeyRecord{}, fmt.Errorf("create api key: %w", err)
	}
	_ = json.Unmarshal(policyJSON, &rec.ToolPolicy)
	return rec, nil
}

func (s *APIKeyStore) List(ctx context.Context) ([]domain.APIKeyRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, key_prefix, tool_policy, created_at FROM api_keys ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []domain.APIKeyRecord
	for rows.Next() {
		var k domain.APIKeyRecord
		var policyJSON []byte
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &policyJSON, &k.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(policyJSON, &k.ToolPolicy)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *APIKeyStore) Delete(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found")
	}
	return nil
}

func (s *APIKeyStore) FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error) {
	var k domain.APIKeyRecord
	var policyJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, key_prefix, tool_policy, created_at FROM api_keys WHERE key_hash = $1
	`, keyHash).Scan(&k.ID, &k.Name, &k.KeyPrefix, &policyJSON, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(policyJSON, &k.ToolPolicy)
	return &k, nil
}
