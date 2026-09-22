package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrVercelUnauthorized is Vercel refusing the stored token ITSELF (a 401 or
// 403), so the application layer can tell a revoked/expired token apart from
// "Vercel is down" without importing the adapter. The adapter satisfies this
// by implementing errors.Is on its API error type, so nothing is wired at
// startup.
var ErrVercelUnauthorized = errors.New("vercel: the stored token was refused")

// VercelProjectLinkStore persists the per-(repository, sub-project) Vercel
// project binding. Keyed by sub-project path because an area cannot name two
// frontends in one monorepo.
type VercelProjectLinkStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.VercelProjectLink, error)
	// "" addresses the repository as a whole.
	Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.VercelProjectLink, error)
	Save(ctx context.Context, link domain.VercelProjectLink) (domain.VercelProjectLink, error)
	Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error
}

// VercelDeploymentsAPI is the deployments slice of Vercel's REST API, kept
// separate from VercelAPI so widening it does not break the hosting service's
// fakes. teamID "" addresses the token owner's personal account.
type VercelDeploymentsAPI interface {
	// target filters by environment ("production"); "" means every target.
	Deployments(ctx context.Context, token, teamID, projectID, target string, limit int) ([]domain.VercelDeployment, error)
}
