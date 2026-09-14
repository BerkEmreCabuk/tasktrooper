package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type MCPSecretRecord struct {
	Location string
	Key      string
	Value    []byte
}

type MCPStore interface {
	List(ctx context.Context) ([]domain.MCPServer, error)
	Get(ctx context.Context, id string) (domain.MCPServer, error)
	Create(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error)
	Update(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
	ListSecrets(ctx context.Context, serverID string) ([]MCPSecretRecord, error)
	SetSecret(ctx context.Context, serverID, location, key string, encrypted []byte) error
	DeleteSecret(ctx context.Context, serverID, location, key string) error
	DeleteSecretsForServer(ctx context.Context, serverID string) error
}
