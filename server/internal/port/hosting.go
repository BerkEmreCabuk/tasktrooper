package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// HostingLinkStore persists the per-(repository, area) provider binding.
type HostingLinkStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.HostingLink, error)
	// Get returns an error wrapping ErrNotFound when the area has no link.
	Get(ctx context.Context, repositoryID uuid.UUID, area string) (domain.HostingLink, error)
	// Save upserts on (repository_id, area).
	Save(ctx context.Context, link domain.HostingLink) (domain.HostingLink, error)
	Delete(ctx context.Context, repositoryID uuid.UUID, area string) error
}

// VercelCredentialStore keeps the Vercel access token (encrypted at rest, the
// way the GitHub token is) and the default team it acts in.
type VercelCredentialStore interface {
	VercelToken(ctx context.Context) (string, error)
	SetVercelToken(ctx context.Context, token string) error
	// DeleteVercelToken removes the token AND the default team: a scope with
	// no credential behind it is not a setting, it is a stale hint.
	DeleteVercelToken(ctx context.Context) error
	VercelTeam(ctx context.Context) (string, error)
	SetVercelTeam(ctx context.Context, teamID string) error
}

// VercelAPI is the slice of Vercel's REST API the hosting service needs. teamID
// "" addresses the token owner's personal account.
type VercelAPI interface {
	User(ctx context.Context, token string) (domain.VercelUser, error)
	Teams(ctx context.Context, token string) ([]domain.VercelTeam, error)
	Projects(ctx context.Context, token, teamID string) ([]domain.VercelProject, error)
	Project(ctx context.Context, token, teamID, idOrName string) (domain.VercelProject, error)
}
