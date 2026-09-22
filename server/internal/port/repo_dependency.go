package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// RepoDependencyStore persists one edge from a repository to a sub-project,
// another repository, or a manually-recorded database.
type RepoDependencyStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepoDependency, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) (outgoing []domain.RepoDependency, incoming []domain.RepoDependency, err error)
	Get(ctx context.Context, id uuid.UUID) (domain.RepoDependency, error)
	Create(ctx context.Context, repositoryID uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error)
	// A "" or masked DatabaseSecret leaves the stored secret untouched.
	Update(ctx context.Context, id uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// Injects the cipher derived at boot, before the environment is scrubbed of
	// MCP_SECRETS_KEY — the same reason RepositoryStore.SetCipher exists.
	SetCipher(c *secrets.Cipher, err error)
}
