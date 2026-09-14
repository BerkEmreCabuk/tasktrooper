package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrVercelUnauthorized is Vercel refusing the stored token ITSELF — a 401 or
// a 403 — as opposed to any other way a call can fail.
//
// It exists so the application layer can tell "the token is there but Vercel
// will not honour it" (revoked, expired, or scoped to nothing the caller asked
// for) apart from "Vercel is down / the network broke", without importing
// internal/adapter/vercel to reach that package's IsUnauthorized. The first is
// a stable answer the operator must go fix in Settings; the second is worth
// retrying. Collapsing them would report a dead token as a 5xx and tell the
// operator to wait for something that will never start working.
//
// The adapter satisfies this by implementing errors.Is on its API error type,
// so nothing has to be wired at startup for the distinction to hold.
var ErrVercelUnauthorized = errors.New("vercel: the stored token was refused")

// VercelProjectLinkStore persists the per-(repository, sub-project) Vercel
// project binding (migration 128).
//
// Keyed by sub-project path rather than by the HostingLinkStore's area,
// because an area cannot name two frontends in one monorepo. The two tables
// coexist the way repository_deploy_targets coexists with
// repository_hosting_links: same provider, different question.
type VercelProjectLinkStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.VercelProjectLink, error)
	// Get returns an error wrapping ErrNotFound when nothing is linked at
	// subProjectPath. "" addresses the repository as a whole.
	Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.VercelProjectLink, error)
	// Save upserts on (repository_id, sub_project_path).
	Save(ctx context.Context, link domain.VercelProjectLink) (domain.VercelProjectLink, error)
	Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error
}

// VercelDeploymentsAPI is the deployments slice of Vercel's REST API, kept
// separate from VercelAPI rather than added to it: VercelAPI is what the
// hosting service and its fakes are written against, and widening an interface
// several fakes implement breaks all of them to serve one new caller.
//
// teamID "" addresses the token owner's personal account.
type VercelDeploymentsAPI interface {
	// Deployments lists a project's most recent deployments, newest first.
	// target filters by environment ("production"); "" means every target.
	Deployments(ctx context.Context, token, teamID, projectID, target string, limit int) ([]domain.VercelDeployment, error)
}
