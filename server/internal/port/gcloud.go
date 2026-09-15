package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrGCloudListingUnavailable is returned, wrapped, when the credential is
// valid but Google will not enumerate a resource family for it. It is a real,
// expected answer rather than a failure: a service account authenticates
// against the token endpoint with no project roles at all, and the API it is
// then pointed at answers 403. The console's next step is to grant a role in
// the customer's own IAM console, not to retry — so callers must present this
// as "connected, cannot list", never as an empty list and never as a 5xx.
//
// The sibling of ErrAppListingUnavailable, and deliberately a separate
// sentinel: the two are produced by different adapters and a caller that
// handles one must not silently absorb the other.
var ErrGCloudListingUnavailable = errors.New("google cloud resource listing is unavailable for this credential")

// GCloudCredentialRow is the stored Google Cloud credential as the database
// holds it: the encrypted payload plus the two plaintext identifiers that name
// the connection without decrypting anything.
type GCloudCredentialRow struct {
	ProjectID   string
	ClientEmail string
	Data        []byte
	UpdatedAt   time.Time
}

// GCloudCredentialStore persists the install's single encrypted Google Cloud
// service-account credential.
type GCloudCredentialStore interface {
	// Set writes the whole credential. projectID and clientEmail are stored in
	// the clear beside encrypted, which carries the key material; the
	// implementation must never persist encrypted's plaintext.
	Set(ctx context.Context, projectID, clientEmail string, encrypted []byte) error
	// Get returns an error wrapping ErrNotFound when nothing is stored.
	Get(ctx context.Context) (GCloudCredentialRow, error)
	Delete(ctx context.Context) error
}

// GCloudResourceStore persists the per-(repository, sub-project) binding to a
// Google Cloud resource.
type GCloudResourceStore interface {
	// Save upserts on (repository_id, sub_project_path).
	Save(ctx context.Context, binding domain.GCloudResourceBinding) (domain.GCloudResourceBinding, error)
	// Get returns an error wrapping ErrNotFound when the scope has no binding.
	// subProjectPath "" addresses the repository itself.
	Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (domain.GCloudResourceBinding, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.GCloudResourceBinding, error)
	Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) error
}

// GCloudClient is the slice of Google Cloud's REST APIs this integration
// speaks. Every method is a read: nothing here creates, updates or deletes a
// customer resource, and that is a property of the interface, not only of
// today's implementation.
//
// A client is built per credential (the service account's signing key lives
// inside it), so no method takes a credential argument.
type GCloudClient interface {
	// Identity is the service account and project this client acts as. The
	// vault stores both in the clear beside the encrypted key, so the console
	// can name the connection without a decrypt — which means SaveCredential
	// has to be able to ask for them after building the client and before
	// persisting anything.
	Identity() domain.GCloudIdentity

	// ValidateAuth confirms the service account actually authenticates. It
	// performs only the OAuth token exchange, so it does not depend on any IAM
	// role having been granted yet — a credential that can sign but cannot
	// list is still a credential worth saving, and SaveCredential's job is to
	// reject key material that is broken, not permissions that are missing.
	ValidateAuth(ctx context.Context) error

	// ListCloudRunServices enumerates the project's Cloud Run services across
	// every region. Returns an error wrapping ErrGCloudListingUnavailable when
	// the credential cannot enumerate at all.
	ListCloudRunServices(ctx context.Context) (domain.GCloudResourceList, error)
	// ListGKEClusters enumerates the project's GKE clusters across every
	// location. Same ErrGCloudListingUnavailable contract.
	//
	// Clusters, not workloads: see domain.GKEClusterDetail for why the
	// Deployments inside a cluster are out of this interface's reach.
	ListGKEClusters(ctx context.Context) (domain.GCloudResourceList, error)

	// CloudRunService reads one service by its fully qualified name. Returns
	// an error wrapping ErrNotFound when the service does not exist or the
	// credential cannot see it.
	CloudRunService(ctx context.Context, name string) (domain.CloudRunServiceDetail, error)
	// GKECluster reads one cluster by its fully qualified name. Same
	// ErrNotFound contract.
	GKECluster(ctx context.Context, name string) (domain.GKEClusterDetail, error)
}
