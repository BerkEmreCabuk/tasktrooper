package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (s *MCPStore) ListSecrets(ctx context.Context, serverID string) ([]port.MCPSecretRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT location, key, encrypted_value
		FROM mcp_server_secrets
		WHERE server_id = $1
		ORDER BY location, key
	`, serverID)
	if err != nil {
		return nil, fmt.Errorf("list mcp secrets: %w", err)
	}
	defer rows.Close()

	var records []port.MCPSecretRecord
	for rows.Next() {
		var rec port.MCPSecretRecord
		if err := rows.Scan(&rec.Location, &rec.Key, &rec.Value); err != nil {
			return nil, fmt.Errorf("scan mcp secret: %w", err)
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

func (s *MCPStore) SetSecret(ctx context.Context, serverID, location, key string, encrypted []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mcp_server_secrets (server_id, location, key, encrypted_value)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_id, location, key)
		DO UPDATE SET encrypted_value = EXCLUDED.encrypted_value, updated_at = now()
	`, serverID, location, key, encrypted)
	if err != nil {
		return fmt.Errorf("set mcp secret: %w", err)
	}
	return nil
}

func (s *MCPStore) DeleteSecret(ctx context.Context, serverID, location, key string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM mcp_server_secrets
		WHERE server_id = $1 AND location = $2 AND key = $3
	`, serverID, location, key)
	if err != nil {
		return fmt.Errorf("delete mcp secret: %w", err)
	}
	return nil
}

func (s *MCPStore) DeleteSecretsForServer(ctx context.Context, serverID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM mcp_server_secrets WHERE server_id = $1`, serverID)
	if err != nil {
		return fmt.Errorf("delete mcp secrets for server: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mapMCPStoreError(err error) error {
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		return domain.ErrMCPServerAlreadyExists
	}
	return err
}
