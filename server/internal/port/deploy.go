package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DeployTargetStore persists the per-(repository, sub-project, env) deploy
// definition. subProjectPath is "" for the repository itself.
type DeployTargetStore interface {
	// ListByRepository returns every target across every sub-project (and the
	// repository itself); callers that want one sub-project's targets filter
	// on DeployTarget.SubProjectPath.
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error)
	// ListAll feeds the production health monitor, which polls every target
	// that declares a health URL across all repositories.
	ListAll(ctx context.Context) ([]domain.DeployTarget, error)
	// Get returns an error wrapping ErrNotFound when no target is defined for
	// (repositoryID, subProjectPath, env) — callers that only want to no-op on
	// "not configured" must check errors.Is(err, ErrNotFound) rather than
	// treating every error as absence, or a real infra failure silently
	// disables whatever the caller was gating behind this lookup.
	Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error)
	// Save upserts on (repository_id, sub_project_path, env).
	Save(ctx context.Context, t domain.DeployTarget) (domain.DeployTarget, error)
	Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error
}
