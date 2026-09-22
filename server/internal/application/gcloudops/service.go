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

type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type Deps struct {
	Credentials port.GCloudCredentialStore
	Bindings    port.GCloudResourceStore
	Cipher      *secrets.Cipher

	NewClient func(domain.GCloudCredential) (port.GCloudClient, error)

	Repos RepositoryResolver
}

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

var ErrInvalidCredential = errors.New("gcloudops: invalid google cloud credential")

var ErrNotConnected = errors.New("gcloudops: google cloud is not connected")

var ErrResourceNotInProject = errors.New("gcloudops: no such resource in this google cloud project")

var ErrUnknownSubProject = errors.New("gcloudops: repository has no such sub-project")

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

func (s *Service) DeleteCredential(ctx context.Context) error {
	if err := s.credentials.Delete(ctx); err != nil {
		return fmt.Errorf("gcloudops: deleting credential: %w", err)
	}
	return nil
}

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

		return nil, errors.New("gcloudops: stored credential is not a valid payload")
	}
	client, err := s.newClient(domain.GCloudCredential{ProjectID: row.ProjectID, Data: data})
	if err != nil {
		return nil, fmt.Errorf("gcloudops: building google cloud client: %w", err)
	}
	return client, nil
}

type FamilyStatus struct {
	Available bool `json:"listing_available"`

	UnreachableLocations []string `json:"unreachable_locations,omitempty"`
}

type ResourceListing struct {
	Resources []domain.GCloudResourceRef `json:"resources"`
	CloudRun  FamilyStatus               `json:"cloud_run"`
	GKE       FamilyStatus               `json:"gke"`
}

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

type BoundResource struct {
	Binding domain.GCloudResourceBinding `json:"binding"`

	Detail *domain.GCloudResourceDetail `json:"detail,omitempty"`
}

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

func (s *Service) Bindings(ctx context.Context, repositoryID uuid.UUID) ([]domain.GCloudResourceBinding, error) {
	out, err := s.bindings.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("gcloudops: listing resource bindings: %w", err)
	}
	return out, nil
}

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

func (s *Service) readRef(ctx context.Context, client port.GCloudClient, resourceType, name string) (domain.GCloudResourceRef, error) {
	detail, err := s.readDetail(ctx, client, resourceType, name)
	if err != nil {
		return domain.GCloudResourceRef{}, err
	}
	return detail.Ref, nil
}

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
