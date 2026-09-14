// Package gcloudops owns the Google Cloud credential vault and the
// per-(repository, sub-project) binding to a Cloud Run service or a GKE
// cluster. It is the read side of a cloud account: nothing here creates,
// scales, deploys or deletes a customer resource.
package gcloudops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// RepositoryResolver reads the repository a binding is scoped to, so a
// sub-project path can be checked against the list that repository actually
// declares. Same shape storeops uses, declared locally so this package does
// not import another application package.
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// Deps wires the collaborators of the Google Cloud operations service.
type Deps struct {
	Credentials port.GCloudCredentialStore
	Bindings    port.GCloudResourceStore
	Cipher      *secrets.Cipher
	// NewClient builds a client from a decrypted credential — swap for a fake
	// in tests, the way storeops swaps NewASC/NewPlay.
	NewClient func(domain.GCloudCredential) (port.GCloudClient, error)
	// Repos is optional. Nil turns off sub-project validation rather than
	// failing every bind: a deployment that has not wired the repository store
	// can still bind a repository-level resource, which is the common case,
	// and refusing would make this service unusable for a missing check.
	Repos RepositoryResolver
}

// Service owns the encrypted Google Cloud credential and the resource
// bindings.
type Service struct {
	credentials port.GCloudCredentialStore
	bindings    port.GCloudResourceStore
	cipher      *secrets.Cipher
	newClient   func(domain.GCloudCredential) (port.GCloudClient, error)
	repos       RepositoryResolver
}

func NewService(d Deps) *Service {
	return &Service{
		credentials: d.Credentials,
		bindings:    d.Bindings,
		cipher:      d.Cipher,
		newClient:   d.NewClient,
		repos:       d.Repos,
	}
}

// ErrInvalidCredential wraps every SaveCredential failure caused by the
// caller's own input — malformed key material, or a service account Google
// itself refuses to issue a token for — as opposed to an infra/config failure
// (cipher not configured, client factory not wired, encrypt/persist error).
// The HTTP layer checks errors.Is to choose 400 vs 500, the same
// sentinel-dispatch idiom storeops uses.
var ErrInvalidCredential = errors.New("gcloudops: invalid google cloud credential")

// ErrNotConnected means nobody has saved a Google Cloud credential yet.
//
// It is deliberately NOT the same answer as port.ErrGCloudListingUnavailable:
// "not connected" sends the operator to the integrations screen to paste a key
// file, while "connected but cannot list" sends them to their own IAM console
// to grant a role. Collapsing the two into one "listing failed" screen makes
// both instructions wrong half the time.
var ErrNotConnected = errors.New("gcloudops: google cloud is not connected")

// ErrResourceNotInProject marks a bind request naming a resource the project
// does not have — the caller's own mistake (400), not a backend failure.
var ErrResourceNotInProject = errors.New("gcloudops: no such resource in this google cloud project")

// ErrUnknownSubProject marks a bind request scoped to a path the repository
// does not declare in sub_projects.
var ErrUnknownSubProject = errors.New("gcloudops: repository has no such sub-project")

// SaveCredential validates data against Google's OAuth token endpoint
// (ValidateAuth) before persisting anything: the vault never holds a
// credential that has not been confirmed to actually authenticate. The payload
// is encrypted before it touches the store, and is never logged — here or
// anywhere data flows through this method.
//
// projectID overrides the key file's own project_id and may be empty, in which
// case the key file decides. It is stored in the clear alongside the service
// account's email so the console can name the connection without a decrypt.
func (s *Service) SaveCredential(ctx context.Context, projectID string, data map[string]string) error {
	if s.cipher == nil {
		return errors.New("gcloudops: secrets cipher not configured")
	}
	if s.newClient == nil {
		return errors.New("gcloudops: google cloud client factory not configured")
	}

	client, err := s.newClient(domain.GCloudCredential{ProjectID: projectID, Data: data})
	if err != nil {
		return fmt.Errorf("gcloudops: building google cloud client: %w: %w", err, ErrInvalidCredential)
	}
	if err := client.ValidateAuth(ctx); err != nil {
		return fmt.Errorf("gcloudops: google cloud credential failed validation: %w: %w", err, ErrInvalidCredential)
	}
	identity := client.Identity()

	plaintext, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("gcloudops: encoding credential: %w", err)
	}
	encrypted, err := s.cipher.Encrypt(string(plaintext))
	if err != nil {
		return fmt.Errorf("gcloudops: encrypting credential: %w", err)
	}
	if err := s.credentials.Set(ctx, identity.ProjectID, identity.ClientEmail, encrypted); err != nil {
		return fmt.Errorf("gcloudops: persisting credential: %w", err)
	}
	return nil
}

// Credential returns the safe-to-serve view: whether Google Cloud is
// connected, which project and service account, and when it was last written.
// The payload is never returned, and a caller has no route to it.
func (s *Service) Credential(ctx context.Context) (domain.GCloudCredentialView, error) {
	row, err := s.credentials.Get(ctx)
	if errors.Is(err, port.ErrNotFound) {
		return domain.GCloudCredentialView{Connected: false}, nil
	}
	if err != nil {
		return domain.GCloudCredentialView{}, fmt.Errorf("gcloudops: reading credential: %w", err)
	}
	return domain.GCloudCredentialView{
		Connected:   true,
		ClientEmail: row.ClientEmail,
		ProjectID:   row.ProjectID,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

// DeleteCredential disconnects Google Cloud. Existing bindings are left
// alone on purpose: they are statements about what a repository IS, and a
// re-connected credential should find them intact rather than making someone
// re-pick every service because a key was rotated.
func (s *Service) DeleteCredential(ctx context.Context) error {
	if err := s.credentials.Delete(ctx); err != nil {
		return fmt.Errorf("gcloudops: deleting credential: %w", err)
	}
	return nil
}

// client decrypts the stored credential and builds a client from it. Returns
// ErrNotConnected when nothing is stored — the one failure the HTTP layer must
// not report as a server error.
func (s *Service) client(ctx context.Context) (port.GCloudClient, error) {
	if s.newClient == nil {
		return nil, errors.New("gcloudops: google cloud client factory not configured")
	}
	if s.cipher == nil {
		return nil, errors.New("gcloudops: secrets cipher not configured")
	}
	row, err := s.credentials.Get(ctx)
	if errors.Is(err, port.ErrNotFound) {
		return nil, ErrNotConnected
	}
	if err != nil {
		return nil, fmt.Errorf("gcloudops: reading credential: %w", err)
	}
	plaintext, err := s.cipher.Decrypt(row.Data)
	if err != nil {
		return nil, fmt.Errorf("gcloudops: decrypting credential: %w", err)
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(plaintext), &data); err != nil {
		// The plaintext is a service account key file. Wrapping the decode
		// error would put a fragment of it into the message, so it does not
		// travel.
		return nil, errors.New("gcloudops: stored credential is not a valid payload")
	}
	client, err := s.newClient(domain.GCloudCredential{ProjectID: row.ProjectID, Data: data})
	if err != nil {
		return nil, fmt.Errorf("gcloudops: building google cloud client: %w", err)
	}
	return client, nil
}

// FamilyStatus is one resource family's listing outcome. Available is stated
// rather than inferred from an empty slice, because "this credential cannot
// enumerate Cloud Run" and "this project has no Cloud Run services" are
// different answers and the console reacts to them differently.
type FamilyStatus struct {
	Available bool `json:"listing_available"`
	// UnreachableLocations names the regions a listing could not read. A
	// partial answer must say so: a silently missing region reads as "you have
	// nothing there", which is how somebody binds the wrong resource.
	UnreachableLocations []string `json:"unreachable_locations,omitempty"`
}

// ResourceListing is everything the picker needs: one flat, type-discriminated
// array plus a per-family verdict, so a credential that can read Cloud Run but
// not GKE still produces a usable picker instead of one error.
type ResourceListing struct {
	Resources []domain.GCloudResourceRef `json:"resources"`
	CloudRun  FamilyStatus               `json:"cloud_run"`
	GKE       FamilyStatus               `json:"gke"`
}

// Resources lists the project's Cloud Run services and GKE clusters.
//
// Returns ErrNotConnected only when no credential is stored at all. A
// credential that is stored but refused by one API yields a successful listing
// with that family marked unavailable — never an error, and never a 5xx at the
// edge: an ungranted IAM role is a state of the customer's project, not a
// fault of this server.
func (s *Service) Resources(ctx context.Context) (ResourceListing, error) {
	client, err := s.client(ctx)
	if err != nil {
		return ResourceListing{}, err
	}

	listing := ResourceListing{Resources: []domain.GCloudResourceRef{}}

	runList, runErr := client.ListCloudRunServices(ctx)
	switch {
	case runErr == nil:
		listing.CloudRun = FamilyStatus{Available: true, UnreachableLocations: runList.UnreachableLocations}
		listing.Resources = append(listing.Resources, runList.Resources...)
	case errors.Is(runErr, port.ErrGCloudListingUnavailable):
		listing.CloudRun = FamilyStatus{Available: false}
	default:
		return ResourceListing{}, fmt.Errorf("gcloudops: listing cloud run services: %w", runErr)
	}

	gkeList, gkeErr := client.ListGKEClusters(ctx)
	switch {
	case gkeErr == nil:
		listing.GKE = FamilyStatus{Available: true, UnreachableLocations: gkeList.UnreachableLocations}
		listing.Resources = append(listing.Resources, gkeList.Resources...)
	case errors.Is(gkeErr, port.ErrGCloudListingUnavailable):
		listing.GKE = FamilyStatus{Available: false}
	default:
		return ResourceListing{}, fmt.Errorf("gcloudops: listing gke clusters: %w", gkeErr)
	}

	return listing, nil
}

// BindResource points one scope of a repository at one Google Cloud resource.
//
// The resource is READ back from Google before the row is written, for the
// same reason storeops confirms a store app exists before linking it: a
// binding the API cannot resolve is a binding every later detail read, and
// every agent that trusts it, fails on. A credential that cannot read the
// resource (403) fails the bind rather than writing an unverified row —
// claiming a binding was confirmed when it was not is the failure mode this
// whole path exists to avoid.
func (s *Service) BindResource(ctx context.Context, repositoryID uuid.UUID, req domain.SaveGCloudResourceRequest) (domain.GCloudResourceBinding, error) {
	req, err := domain.ValidateSaveGCloudResource(req)
	if err != nil {
		return domain.GCloudResourceBinding{}, err
	}
	if err := s.checkSubProject(ctx, repositoryID, req.SubProjectPath); err != nil {
		return domain.GCloudResourceBinding{}, err
	}

	client, err := s.client(ctx)
	if err != nil {
		return domain.GCloudResourceBinding{}, err
	}
	ref, err := s.readRef(ctx, client, req.ResourceType, req.ResourceName)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return domain.GCloudResourceBinding{}, fmt.Errorf("gcloudops: %s: %w", req.ResourceName, ErrResourceNotInProject)
		}
		return domain.GCloudResourceBinding{}, err
	}

	// Google's own answer wins over the request body for everything but the
	// scope: the picker's copy can be minutes stale, and the location a detail
	// read is later issued against must be the one the resource actually has.
	saved, err := s.bindings.Save(ctx, domain.GCloudResourceBinding{
		RepositoryID:   repositoryID,
		SubProjectPath: req.SubProjectPath,
		ResourceType:   ref.Type,
		ResourceName:   ref.Name,
		DisplayName:    ref.DisplayName,
		ProjectID:      ref.ProjectID,
		Location:       ref.Location,
		Source:         "user",
	})
	if err != nil {
		return domain.GCloudResourceBinding{}, fmt.Errorf("gcloudops: saving resource binding: %w", err)
	}
	return saved, nil
}

// BoundResource is a binding plus what Google says about it right now.
type BoundResource struct {
	Binding domain.GCloudResourceBinding `json:"binding"`
	// Detail is nil when the credential could not read the resource. The
	// binding still stands — the console shows what it points at and why the
	// live view is missing, rather than dropping the row.
	Detail *domain.GCloudResourceDetail `json:"detail,omitempty"`
}

// Resource returns one scope's binding and, when the credential can read it,
// the live detail behind it.
//
// port.ErrNotFound means one of two things and the message says which: the
// scope has no binding, or the binding points at a resource Google no longer
// has. Both are 404 — the second is not dressed up as a permissions problem,
// because the fix is to re-bind, not to grant a role.
func (s *Service) Resource(ctx context.Context, repositoryID uuid.UUID, subProjectPath string) (BoundResource, error) {
	binding, err := s.bindings.Get(ctx, repositoryID, subProjectPath)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return BoundResource{}, fmt.Errorf("gcloudops: this scope has no google cloud resource bound: %w", port.ErrNotFound)
		}
		return BoundResource{}, fmt.Errorf("gcloudops: reading resource binding: %w", err)
	}

	client, err := s.client(ctx)
	if err != nil {
		// A stored binding with no credential behind it is still the truth
		// about the repository, so it is returned; only the live view is
		// missing, and ErrNotConnected is what tells the edge to say so.
		return BoundResource{Binding: binding}, err
	}

	detail, err := s.readDetail(ctx, client, binding.ResourceType, binding.ResourceName)
	if err != nil {
		if errors.Is(err, port.ErrGCloudListingUnavailable) {
			return BoundResource{Binding: binding}, err
		}
		if errors.Is(err, port.ErrNotFound) {
			return BoundResource{Binding: binding}, fmt.Errorf("gcloudops: %s no longer exists in project %s: %w", binding.ResourceName, binding.ProjectID, port.ErrNotFound)
		}
		return BoundResource{Binding: binding}, fmt.Errorf("gcloudops: reading google cloud resource: %w", err)
	}
	return BoundResource{Binding: binding, Detail: &detail}, nil
}

// Bindings lists every scope of a repository that has a resource bound.
func (s *Service) Bindings(ctx context.Context, repositoryID uuid.UUID) ([]domain.GCloudResourceBinding, error) {
	out, err := s.bindings.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("gcloudops: listing resource bindings: %w", err)
	}
	return out, nil
}

// readDetail dispatches a detail read on the resource type.
func (s *Service) readDetail(ctx context.Context, client port.GCloudClient, resourceType, name string) (domain.GCloudResourceDetail, error) {
	switch resourceType {
	case domain.GCloudResourceCloudRun:
		svc, err := client.CloudRunService(ctx, name)
		if err != nil {
			return domain.GCloudResourceDetail{}, err
		}
		return domain.GCloudResourceDetail{Ref: svc.Ref, CloudRun: &svc}, nil
	case domain.GCloudResourceGKECluster:
		cluster, err := client.GKECluster(ctx, name)
		if err != nil {
			return domain.GCloudResourceDetail{}, err
		}
		return domain.GCloudResourceDetail{Ref: cluster.Ref, GKECluster: &cluster}, nil
	default:
		return domain.GCloudResourceDetail{}, fmt.Errorf("%w: unknown resource type %q", domain.ErrInvalidGCloudResource, resourceType)
	}
}

// readRef is readDetail reduced to the identity a binding stores.
func (s *Service) readRef(ctx context.Context, client port.GCloudClient, resourceType, name string) (domain.GCloudResourceRef, error) {
	detail, err := s.readDetail(ctx, client, resourceType, name)
	if err != nil {
		return domain.GCloudResourceRef{}, err
	}
	return detail.Ref, nil
}

// checkSubProject refuses a binding scoped to a path the repository does not
// declare. "" is the repository itself and is always allowed.
func (s *Service) checkSubProject(ctx context.Context, repositoryID uuid.UUID, path string) error {
	if path == "" || s.repos == nil {
		return nil
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("gcloudops: reading repository: %w", err)
	}
	for _, sp := range repo.SubProjects {
		if sp.Path == path {
			return nil
		}
	}
	return fmt.Errorf("gcloudops: %q: %w", path, ErrUnknownSubProject)
}
