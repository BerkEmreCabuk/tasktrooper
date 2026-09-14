package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type LLMEndpointStore struct {
	pool *DB
}

func NewLLMEndpointStore(pool *DB) *LLMEndpointStore {
	return &LLMEndpointStore{pool: pool}
}

const llmEndpointSelect = `
	SELECT e.id, e.name, e.base_url, e.default_model, e.timeout_seconds, e.configured,
	       e.created_at, e.updated_at,
	       (s.api_key_encrypted IS NOT NULL AND length(s.api_key_encrypted) > 0) AS has_api_key
	FROM llm_endpoints e
	LEFT JOIN llm_endpoint_secrets s ON s.endpoint_id = e.id
`

func scanEndpoint(row pgx.Row) (domain.LLMEndpoint, error) {
	var ep domain.LLMEndpoint
	err := row.Scan(
		&ep.ID, &ep.Name, &ep.BaseURL, &ep.DefaultModel, &ep.TimeoutSeconds, &ep.Configured,
		&ep.CreatedAt, &ep.UpdatedAt, &ep.HasAPIKey,
	)
	return ep, err
}

func (s *LLMEndpointStore) List(ctx context.Context) ([]domain.LLMEndpoint, error) {
	rows, err := s.pool.Query(ctx, llmEndpointSelect+` ORDER BY e.created_at`)
	if err != nil {
		return nil, fmt.Errorf("list llm endpoints: %w", err)
	}
	defer rows.Close()

	var out []domain.LLMEndpoint
	for rows.Next() {
		ep, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *LLMEndpointStore) Get(ctx context.Context, id string) (domain.LLMEndpoint, error) {
	ep, err := scanEndpoint(s.pool.QueryRow(ctx, llmEndpointSelect+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LLMEndpoint{}, fmt.Errorf("llm endpoint not found: %s", id)
	}
	if err != nil {
		return domain.LLMEndpoint{}, fmt.Errorf("get llm endpoint: %w", err)
	}
	return ep, nil
}

func (s *LLMEndpointStore) Create(ctx context.Context, ep domain.LLMEndpoint) (domain.LLMEndpoint, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO llm_endpoints (name, base_url, default_model, timeout_seconds, configured)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, ep.Name, ep.BaseURL, ep.DefaultModel, ep.TimeoutSeconds, ep.Configured).Scan(&id)
	if err != nil {
		return domain.LLMEndpoint{}, fmt.Errorf("create llm endpoint: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *LLMEndpointStore) Update(ctx context.Context, ep domain.LLMEndpoint) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE llm_endpoints
		SET name = $2, base_url = $3, default_model = $4, timeout_seconds = $5, configured = $6, updated_at = now()
		WHERE id = $1
	`, ep.ID, ep.Name, ep.BaseURL, ep.DefaultModel, ep.TimeoutSeconds, ep.Configured)
	if err != nil {
		return fmt.Errorf("update llm endpoint: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("llm endpoint not found: %s", ep.ID)
	}
	return nil
}

func (s *LLMEndpointStore) Delete(ctx context.Context, id string) error {
	// llm_endpoint_secrets rows cascade on delete.
	_, err := s.pool.Exec(ctx, `DELETE FROM llm_endpoints WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete llm endpoint: %w", err)
	}
	return nil
}

func (s *LLMEndpointStore) SetAPIKey(ctx context.Context, id string, encrypted []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_endpoint_secrets (endpoint_id, api_key_encrypted, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tenant_id, endpoint_id) DO UPDATE SET
			api_key_encrypted = EXCLUDED.api_key_encrypted,
			updated_at = now()
	`, id, encrypted)
	if err != nil {
		return fmt.Errorf("set llm endpoint api key: %w", err)
	}
	return nil
}

func (s *LLMEndpointStore) GetAPIKeyEncrypted(ctx context.Context, id string) ([]byte, error) {
	var encrypted []byte
	err := s.pool.QueryRow(ctx, `
		SELECT api_key_encrypted FROM llm_endpoint_secrets WHERE endpoint_id = $1
	`, id).Scan(&encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get llm endpoint api key: %w", err)
	}
	return encrypted, nil
}

func (s *LLMEndpointStore) DeleteAPIKey(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM llm_endpoint_secrets WHERE endpoint_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete llm endpoint api key: %w", err)
	}
	return nil
}
