package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type LLMProviderStore struct {
	pool *DB
}

func NewLLMProviderStore(pool *DB) *LLMProviderStore {
	return &LLMProviderStore{pool: pool}
}

func (s *LLMProviderStore) List(ctx context.Context) ([]domain.LLMProviderConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.provider_type, c.base_url, c.default_model, c.timeout_seconds, c.configured, c.updated_at,
		       (s.api_key_encrypted IS NOT NULL AND length(s.api_key_encrypted) > 0) AS has_api_key
		FROM llm_provider_configs c
		LEFT JOIN llm_provider_secrets s ON s.provider_type = c.provider_type
		ORDER BY c.provider_type
	`)
	if err != nil {
		return nil, fmt.Errorf("list llm providers: %w", err)
	}
	defer rows.Close()

	var out []domain.LLMProviderConfig
	for rows.Next() {
		var cfg domain.LLMProviderConfig
		if err := rows.Scan(
			&cfg.ProviderType, &cfg.BaseURL, &cfg.DefaultModel, &cfg.TimeoutSeconds, &cfg.Configured, &cfg.UpdatedAt, &cfg.HasAPIKey,
		); err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, rows.Err()
}

func (s *LLMProviderStore) Get(ctx context.Context, providerType domain.LLMProviderType) (domain.LLMProviderConfig, error) {
	var cfg domain.LLMProviderConfig
	err := s.pool.QueryRow(ctx, `
		SELECT c.provider_type, c.base_url, c.default_model, c.timeout_seconds, c.configured, c.updated_at,
		       (s.api_key_encrypted IS NOT NULL AND length(s.api_key_encrypted) > 0) AS has_api_key
		FROM llm_provider_configs c
		LEFT JOIN llm_provider_secrets s ON s.provider_type = c.provider_type
		WHERE c.provider_type = $1
	`, string(providerType)).Scan(
		&cfg.ProviderType, &cfg.BaseURL, &cfg.DefaultModel, &cfg.TimeoutSeconds, &cfg.Configured, &cfg.UpdatedAt, &cfg.HasAPIKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LLMProviderConfig{}, fmt.Errorf("llm provider not found: %s", providerType)
	}
	if err != nil {
		return domain.LLMProviderConfig{}, fmt.Errorf("get llm provider: %w", err)
	}
	return cfg, nil
}

func (s *LLMProviderStore) Upsert(ctx context.Context, cfg domain.LLMProviderConfig) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_provider_configs (provider_type, base_url, default_model, timeout_seconds, configured, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (tenant_id, provider_type) DO UPDATE SET
			base_url = EXCLUDED.base_url,
			default_model = EXCLUDED.default_model,
			timeout_seconds = EXCLUDED.timeout_seconds,
			configured = EXCLUDED.configured,
			updated_at = now()
	`, string(cfg.ProviderType), cfg.BaseURL, cfg.DefaultModel, cfg.TimeoutSeconds, cfg.Configured)
	if err != nil {
		return fmt.Errorf("upsert llm provider: %w", err)
	}
	return nil
}

func (s *LLMProviderStore) SetAPIKey(ctx context.Context, providerType domain.LLMProviderType, encrypted []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_provider_secrets (provider_type, api_key_encrypted, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tenant_id, provider_type) DO UPDATE SET
			api_key_encrypted = EXCLUDED.api_key_encrypted,
			updated_at = now()
	`, string(providerType), encrypted)
	if err != nil {
		return fmt.Errorf("set llm provider api key: %w", err)
	}
	return nil
}

func (s *LLMProviderStore) GetAPIKeyEncrypted(ctx context.Context, providerType domain.LLMProviderType) ([]byte, error) {
	var encrypted []byte
	err := s.pool.QueryRow(ctx, `
		SELECT api_key_encrypted FROM llm_provider_secrets WHERE provider_type = $1
	`, string(providerType)).Scan(&encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get llm provider api key: %w", err)
	}
	return encrypted, nil
}

func (s *LLMProviderStore) DeleteAPIKey(ctx context.Context, providerType domain.LLMProviderType) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM llm_provider_secrets WHERE provider_type = $1`, string(providerType))
	if err != nil {
		return fmt.Errorf("delete llm provider api key: %w", err)
	}
	return nil
}

func (s *LLMProviderStore) GetActiveProvider(ctx context.Context) (domain.LLMProviderType, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'active_llm_provider'`).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LLMProviderLocal, nil
	}
	if err != nil {
		return "", fmt.Errorf("get active llm provider: %w", err)
	}
	// The value is EITHER a native provider type OR an endpoint uuid; return it
	// as-is (resolution is by client-map lookup). Only empty falls back to local.
	if value == "" {
		return domain.LLMProviderLocal, nil
	}
	return domain.LLMProviderType(value), nil
}

func (s *LLMProviderStore) SetActiveProvider(ctx context.Context, providerType domain.LLMProviderType) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ('active_llm_provider', $1, now())
		ON CONFLICT (tenant_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, string(providerType))
	if err != nil {
		return fmt.Errorf("set active llm provider: %w", err)
	}
	return nil
}

// GetEmbeddingProvider returns the provider pinned for embeddings, or "" (auto).
func (s *LLMProviderStore) GetEmbeddingProvider(ctx context.Context) (domain.LLMProviderType, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'embedding_llm_provider'`).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get embedding llm provider: %w", err)
	}
	// EITHER a native provider type, an endpoint uuid, or "" (auto). Returned
	// as-is; unknown refs are ignored at resolution time, not here.
	return domain.LLMProviderType(value), nil
}

func (s *LLMProviderStore) SetEmbeddingProvider(ctx context.Context, providerType domain.LLMProviderType) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ('embedding_llm_provider', $1, now())
		ON CONFLICT (tenant_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, string(providerType))
	if err != nil {
		return fmt.Errorf("set embedding llm provider: %w", err)
	}
	return nil
}

func (s *LLMProviderStore) GetEmbeddingModel(ctx context.Context) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'embedding_llm_model'`).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get embedding llm model: %w", err)
	}
	return value, nil
}

func (s *LLMProviderStore) SetEmbeddingModel(ctx context.Context, model string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ('embedding_llm_model', $1, now())
		ON CONFLICT (tenant_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, model)
	if err != nil {
		return fmt.Errorf("set embedding llm model: %w", err)
	}
	return nil
}
