package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var ErrGCloudListingUnavailable = errors.New("google cloud resource listing is unavailable for this credential")

type GCloudCredentialRow struct {
	ProjectID   string
	ClientEmail string
	Data        []byte
	UpdatedAt   time.Time
}

type GCloudCredentialStore interface {
	Set(ctx context.Context, projectID, clientEmail string, encrypted []byte) error
	Get(ctx context.Context) (GCloudCredentialRow, error)
	Delete(ctx context.Context) error
}

type GCloudResourceStore interface {
	Save(ctx context.Context, binding domain.GCloudResourceBinding) (domain.GCloudResourceBinding, error)
	// subProjectPath "" addresses the repository itself.
	Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.GCloudResourceBinding, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.GCloudResourceBinding, error)
	Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error
}

type GCloudClient interface {
	Identity() domain.GCloudIdentity

	ValidateAuth(ctx context.Context) error

	ListCloudRunServices(ctx context.Context) (domain.GCloudResourceList, error)
	ListGKEClusters(ctx context.Context) (domain.GCloudResourceList, error)

	CloudRunService(ctx context.Context, name string) (domain.CloudRunServiceDetail, error)
	GKECluster(ctx context.Context, name string) (domain.GKEClusterDetail, error)
}
