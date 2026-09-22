package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DeployTargetStore persists the per-(repository, sub-project, env) deploy
// definition; subProjectPath is "" for the repository itself.
type DeployTargetStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error)
	// ListAll feeds the production health monitor, which polls every target
	// that declares a health URL.
	ListAll(ctx context.Context) ([]domain.DeployTarget, error)
	// Returns an error wrapping ErrNotFound when no target is configured;
	// callers that want to no-op on "not configured" must check with
	// errors.Is, or a real infra failure silently disables their gate.
	Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error)
	Save(ctx context.Context, t domain.DeployTarget) (domain.DeployTarget, error)
	Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error
}
