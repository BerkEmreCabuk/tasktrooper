package gcloudops_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/gcloudops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeCredentialStore stores whatever bytes it is handed untouched (no
// encryption of its own), so a test can prove the SERVICE is what encrypts.
type fakeCredentialStore struct {
	row     port.GCloudCredentialRow
	present bool
	setErr  error
}

var _ port.GCloudCredentialStore = (*fakeCredentialStore)(nil)

func (f *fakeCredentialStore) Set(_ context.Context, projectID, clientEmail string, encrypted []byte) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.row = port.GCloudCredentialRow{ProjectID: projectID, ClientEmail: clientEmail, Data: encrypted, UpdatedAt: time.Now()}
	f.present = true
	return nil
}

func (f *fakeCredentialStore) Get(_ context.Context) (port.GCloudCredentialRow, error) {
	if !f.present {
		return port.GCloudCredentialRow{}, port.ErrNotFound
	}
	return f.row, nil
}

func (f *fakeCredentialStore) Delete(_ context.Context) error {
	f.present = false
	f.row = port.GCloudCredentialRow{}
	return nil
}

// fakeBindingStore is an in-memory port.GCloudResourceStore keyed the way the
// postgres unique constraint is.
type fakeBindingStore struct {
	rows map[string]domain.GCloudResourceBinding
}

var _ port.GCloudResourceStore = (*fakeBindingStore)(nil)

func newFakeBindingStore() *fakeBindingStore {
	return &fakeBindingStore{rows: map[string]domain.GCloudResourceBinding{}}
}

func bindingKey(repositoryID uuid.UUID, path string) string {
	return repositoryID.String() + "/" + path
}

func (f *fakeBindingStore) Save(_ context.Context, b domain.GCloudResourceBinding) (domain.GCloudResourceBinding, error) {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	b.UpdatedAt = time.Now()
	f.rows[bindingKey(b.RepositoryID, b.SubProjectPath)] = b
	return b, nil
}

func (f *fakeBindingStore) Get(_ context.Context, repositoryID uuid.UUID, path string) (domain.GCloudResourceBinding, error) {
	b, ok := f.rows[bindingKey(repositoryID, path)]
	if !ok {
		return domain.GCloudResourceBinding{}, port.ErrNotFound
	}
	return b, nil
}

func (f *fakeBindingStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.GCloudResourceBinding, error) {
	var out []domain.GCloudResourceBinding
	for _, b := range f.rows {
		if b.RepositoryID == repositoryID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeBindingStore) Delete(_ context.Context, repositoryID uuid.UUID, path string) error {
	delete(f.rows, bindingKey(repositoryID, path))
	return nil
}

// fakeClient is a scriptable port.GCloudClient.
type fakeClient struct {
	identity  domain.GCloudIdentity
	validErr  error
	runList   domain.GCloudResourceList
	runErr    error
	gkeList   domain.GCloudResourceList
	gkeErr    error
	runDetail domain.CloudRunServiceDetail
	getErr    error
	gkeDetail domain.GKEClusterDetail
}

var _ port.GCloudClient = (*fakeClient)(nil)

func (f *fakeClient) Identity() domain.GCloudIdentity    { return f.identity }
func (f *fakeClient) ValidateAuth(context.Context) error { return f.validErr }
func (f *fakeClient) ListCloudRunServices(context.Context) (domain.GCloudResourceList, error) {
	return f.runList, f.runErr
}
func (f *fakeClient) ListGKEClusters(context.Context) (domain.GCloudResourceList, error) {
	return f.gkeList, f.gkeErr
}
func (f *fakeClient) CloudRunService(_ context.Context, name string) (domain.CloudRunServiceDetail, error) {
	if f.getErr != nil {
		return domain.CloudRunServiceDetail{}, f.getErr
	}
	detail := f.runDetail
	if detail.Ref.Name == "" {
		detail.Ref = domain.GCloudResourceRef{Type: domain.GCloudResourceCloudRun, Name: name}
	}
	return detail, nil
}
func (f *fakeClient) GKECluster(_ context.Context, name string) (domain.GKEClusterDetail, error) {
	if f.getErr != nil {
		return domain.GKEClusterDetail{}, f.getErr
	}
	detail := f.gkeDetail
	if detail.Ref.Name == "" {
		detail.Ref = domain.GCloudResourceRef{Type: domain.GCloudResourceGKECluster, Name: name}
	}
	return detail, nil
}

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	c, err := secrets.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const testKeyFile = `{"client_email":"sa@demo.iam.gserviceaccount.com","private_key":"pretend","project_id":"demo-project"}`

func newService(t *testing.T, creds *fakeCredentialStore, bindings *fakeBindingStore, client *fakeClient) *gcloudops.Service {
	t.Helper()
	return gcloudops.NewService(gcloudops.Deps{
		Credentials: creds,
		Bindings:    bindings,
		Cipher:      testCipher(t),
		NewClient: func(domain.GCloudCredential) (port.GCloudClient, error) {
			return client, nil
		},
	})
}

// TestSaveCredentialEncryptsThePayload is the storage invariant: the key file
// must never sit in the database in the clear.
func TestSaveCredentialEncryptsThePayload(t *testing.T) {
	creds := &fakeCredentialStore{}
	client := &fakeClient{identity: domain.GCloudIdentity{ProjectID: "demo-project", ClientEmail: "sa@demo.iam.gserviceaccount.com"}}
	svc := newService(t, creds, newFakeBindingStore(), client)

	if err := svc.SaveCredential(context.Background(), "", map[string]string{"service_account_json": testKeyFile}); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	if !creds.present {
		t.Fatal("nothing was persisted")
	}
	if strings.Contains(string(creds.row.Data), "private_key") || strings.Contains(string(creds.row.Data), "pretend") {
		t.Fatalf("the stored payload is not encrypted: %q", creds.row.Data)
	}
	// The two identifiers ARE stored in the clear, on purpose: the console
	// names the connection without a decrypt.
	if creds.row.ProjectID != "demo-project" || creds.row.ClientEmail != "sa@demo.iam.gserviceaccount.com" {
		t.Fatalf("identity columns = %q / %q", creds.row.ProjectID, creds.row.ClientEmail)
	}
}

// TestSaveCredentialRefusesToStoreAnUnvalidatedCredential is storeops' rule
// applied here: nothing reaches the vault that has not been confirmed to
// authenticate.
func TestSaveCredentialRefusesToStoreAnUnvalidatedCredential(t *testing.T) {
	creds := &fakeCredentialStore{}
	client := &fakeClient{validErr: errors.New("invalid_grant")}
	svc := newService(t, creds, newFakeBindingStore(), client)

	err := svc.SaveCredential(context.Background(), "", map[string]string{"service_account_json": testKeyFile})
	if !errors.Is(err, gcloudops.ErrInvalidCredential) {
		t.Fatalf("err = %v, want it to wrap ErrInvalidCredential", err)
	}
	if creds.present {
		t.Fatal("an unvalidated credential reached the vault")
	}
}

// TestSaveCredentialReportsAPersistFailureAsInfra keeps the 400/500 split
// honest: a database failure is not the caller's bad input.
func TestSaveCredentialReportsAPersistFailureAsInfra(t *testing.T) {
	creds := &fakeCredentialStore{setErr: errors.New("connection refused")}
	svc := newService(t, creds, newFakeBindingStore(), &fakeClient{})

	err := svc.SaveCredential(context.Background(), "", map[string]string{"service_account_json": testKeyFile})
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, gcloudops.ErrInvalidCredential) {
		t.Fatalf("a persist failure was reported as invalid input: %v", err)
	}
}

// TestCredentialNeverReturnsThePayload is the disclosure invariant: the view
// carries identifiers and a timestamp, and there is no route back to the key.
func TestCredentialNeverReturnsThePayload(t *testing.T) {
	creds := &fakeCredentialStore{}
	client := &fakeClient{identity: domain.GCloudIdentity{ProjectID: "demo-project", ClientEmail: "sa@demo.iam.gserviceaccount.com"}}
	svc := newService(t, creds, newFakeBindingStore(), client)

	if err := svc.SaveCredential(context.Background(), "", map[string]string{"service_account_json": testKeyFile}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if !view.Connected || view.ProjectID != "demo-project" {
		t.Fatalf("view = %+v", view)
	}
	if view.UpdatedAt.IsZero() {
		t.Fatal("updated_at was not reported")
	}
}

func TestCredentialReportsDisconnectedWithoutAnError(t *testing.T) {
	svc := newService(t, &fakeCredentialStore{}, newFakeBindingStore(), &fakeClient{})
	view, err := svc.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if view.Connected {
		t.Fatal("an empty vault reported itself as connected")
	}
}

// TestResourcesReportsNotConnectedSeparately is the sentinel the edge branches
// on to answer not_connected rather than listing_unsupported.
func TestResourcesReportsNotConnectedSeparately(t *testing.T) {
	svc := newService(t, &fakeCredentialStore{}, newFakeBindingStore(), &fakeClient{})
	_, err := svc.Resources(context.Background())
	if !errors.Is(err, gcloudops.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

// connectedService saves a credential and hands back the service, so the
// listing tests start from a connected vault.
func connectedService(t *testing.T, client *fakeClient, bindings *fakeBindingStore) *gcloudops.Service {
	t.Helper()
	creds := &fakeCredentialStore{}
	svc := newService(t, creds, bindings, client)
	if err := svc.SaveCredential(context.Background(), "", map[string]string{"service_account_json": testKeyFile}); err != nil {
		t.Fatal(err)
	}
	return svc
}

// TestResourcesKeepsOneFamilyWhenTheOtherIsRefused is the reason the verdict
// is per-family: a service account with roles/run.viewer and nothing else
// still produces a usable Cloud Run picker.
func TestResourcesKeepsOneFamilyWhenTheOtherIsRefused(t *testing.T) {
	client := &fakeClient{
		runList: domain.GCloudResourceList{Resources: []domain.GCloudResourceRef{
			{Type: domain.GCloudResourceCloudRun, Name: "projects/demo-project/locations/us-central1/services/api"},
		}},
		gkeErr: port.ErrGCloudListingUnavailable,
	}
	svc := connectedService(t, client, newFakeBindingStore())

	listing, err := svc.Resources(context.Background())
	if err != nil {
		t.Fatalf("Resources: %v", err)
	}
	if !listing.CloudRun.Available {
		t.Fatal("cloud run should still be listable")
	}
	if listing.GKE.Available {
		t.Fatal("gke reported available after a refusal")
	}
	if len(listing.Resources) != 1 {
		t.Fatalf("resources = %d, want the one cloud run service", len(listing.Resources))
	}
}

// TestResourcesNeverTurnsARefusalIntoAnError is the "no 500" rule: both
// families refused is still a successful, connected answer.
func TestResourcesNeverTurnsARefusalIntoAnError(t *testing.T) {
	client := &fakeClient{runErr: port.ErrGCloudListingUnavailable, gkeErr: port.ErrGCloudListingUnavailable}
	svc := connectedService(t, client, newFakeBindingStore())

	listing, err := svc.Resources(context.Background())
	if err != nil {
		t.Fatalf("Resources returned an error for a permissions state: %v", err)
	}
	if listing.CloudRun.Available || listing.GKE.Available {
		t.Fatal("a refused family reported itself available")
	}
	if listing.Resources == nil {
		t.Fatal("resources is nil; it must be an empty array")
	}
}

// TestResourcesPropagatesARealFailure keeps the other side of that line: a
// broken credential or a network failure is a genuine error.
func TestResourcesPropagatesARealFailure(t *testing.T) {
	client := &fakeClient{runErr: errors.New("connection reset")}
	svc := connectedService(t, client, newFakeBindingStore())

	if _, err := svc.Resources(context.Background()); err == nil {
		t.Fatal("expected a real transport failure to surface")
	}
}

func TestResourcesCarriesUnreachableLocations(t *testing.T) {
	client := &fakeClient{
		runList: domain.GCloudResourceList{UnreachableLocations: []string{"asia-east1"}},
		gkeList: domain.GCloudResourceList{UnreachableLocations: []string{"us-east1-b"}},
	}
	svc := connectedService(t, client, newFakeBindingStore())

	listing, err := svc.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.CloudRun.UnreachableLocations) != 1 || len(listing.GKE.UnreachableLocations) != 1 {
		t.Fatalf("unreachable locations were dropped: %+v", listing)
	}
}

// TestBindResourceConfirmsTheResourceExists mirrors storeops' rule: a binding
// nothing can resolve is a binding every later read fails on.
func TestBindResourceConfirmsTheResourceExists(t *testing.T) {
	client := &fakeClient{getErr: port.ErrNotFound}
	bindings := newFakeBindingStore()
	svc := connectedService(t, client, bindings)

	repoID := uuid.New()
	_, err := svc.BindResource(context.Background(), repoID, domain.SaveGCloudResourceRequest{
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/ghost",
	})
	if !errors.Is(err, gcloudops.ErrResourceNotInProject) {
		t.Fatalf("err = %v, want ErrResourceNotInProject", err)
	}
	if len(bindings.rows) != 0 {
		t.Fatal("an unverified binding was written")
	}
}

// TestBindResourceRefusesWhenItCannotVerify: a 403 means the credential
// cannot read the resource, so the binding cannot be claimed as confirmed.
func TestBindResourceRefusesWhenItCannotVerify(t *testing.T) {
	client := &fakeClient{getErr: port.ErrGCloudListingUnavailable}
	bindings := newFakeBindingStore()
	svc := connectedService(t, client, bindings)

	_, err := svc.BindResource(context.Background(), uuid.New(), domain.SaveGCloudResourceRequest{
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
	})
	if !errors.Is(err, port.ErrGCloudListingUnavailable) {
		t.Fatalf("err = %v, want the listing-unavailable sentinel", err)
	}
	if len(bindings.rows) != 0 {
		t.Fatal("a binding was written without verification")
	}
}

// TestBindResourceStoresGooglesAnswerNotTheRequest: the picker's copy can be
// stale, and the location decides which resource a later read reaches.
func TestBindResourceStoresGooglesAnswerNotTheRequest(t *testing.T) {
	client := &fakeClient{runDetail: domain.CloudRunServiceDetail{
		Ref: domain.GCloudResourceRef{
			Type:        domain.GCloudResourceCloudRun,
			Name:        "projects/demo-project/locations/europe-west1/services/worker",
			DisplayName: "worker",
			ProjectID:   "demo-project",
			Location:    "europe-west1",
		},
	}}
	bindings := newFakeBindingStore()
	svc := connectedService(t, client, bindings)

	repoID := uuid.New()
	saved, err := svc.BindResource(context.Background(), repoID, domain.SaveGCloudResourceRequest{
		SubProjectPath: "services/worker",
		ResourceType:   domain.GCloudResourceCloudRun,
		ResourceName:   "projects/demo-project/locations/europe-west1/services/worker",
		Location:       "us-central1",
	})
	if err != nil {
		t.Fatalf("BindResource: %v", err)
	}
	if saved.Location != "europe-west1" {
		t.Fatalf("location = %q, want Google's answer to win over the request body", saved.Location)
	}
	if saved.SubProjectPath != "services/worker" {
		t.Fatalf("sub project = %q", saved.SubProjectPath)
	}
	if saved.DisplayName != "worker" {
		t.Fatalf("display name = %q", saved.DisplayName)
	}
}

func TestBindResourceRejectsAShortResourceName(t *testing.T) {
	svc := connectedService(t, &fakeClient{}, newFakeBindingStore())
	_, err := svc.BindResource(context.Background(), uuid.New(), domain.SaveGCloudResourceRequest{
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "api",
	})
	if !errors.Is(err, domain.ErrInvalidGCloudResource) {
		t.Fatalf("err = %v, want ErrInvalidGCloudResource", err)
	}
}

func TestBindResourceRejectsAMismatchedType(t *testing.T) {
	svc := connectedService(t, &fakeClient{}, newFakeBindingStore())
	_, err := svc.BindResource(context.Background(), uuid.New(), domain.SaveGCloudResourceRequest{
		ResourceType: domain.GCloudResourceGKECluster,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
	})
	if !errors.Is(err, domain.ErrInvalidGCloudResource) {
		t.Fatalf("err = %v, want a type/name mismatch to be rejected", err)
	}
}

// TestBindResourceRejectsAnUnknownSubProject exercises the optional repository
// resolver: a path the repository does not declare is the caller's mistake.
func TestBindResourceRejectsAnUnknownSubProject(t *testing.T) {
	repoID := uuid.New()
	svc := gcloudops.NewService(gcloudops.Deps{
		Credentials: &fakeCredentialStore{},
		Bindings:    newFakeBindingStore(),
		Cipher:      testCipher(t),
		NewClient:   func(domain.GCloudCredential) (port.GCloudClient, error) { return &fakeClient{}, nil },
		Repos:       fakeRepos{repo: domain.Repository{ID: repoID, SubProjects: []domain.RepoSubProject{{Path: "services/api", Kind: domain.RepoKindBackend}}}},
	})

	_, err := svc.BindResource(context.Background(), repoID, domain.SaveGCloudResourceRequest{
		SubProjectPath: "services/nope",
		ResourceType:   domain.GCloudResourceCloudRun,
		ResourceName:   "projects/demo-project/locations/us-central1/services/api",
	})
	if !errors.Is(err, gcloudops.ErrUnknownSubProject) {
		t.Fatalf("err = %v, want ErrUnknownSubProject", err)
	}
}

type fakeRepos struct{ repo domain.Repository }

func (f fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) { return f.repo, nil }

// TestResourceReturnsTheBindingEvenWhenDisconnected: the binding is a fact
// about the repository, and losing it because a key was deleted would lose
// real state.
func TestResourceReturnsTheBindingEvenWhenDisconnected(t *testing.T) {
	bindings := newFakeBindingStore()
	repoID := uuid.New()
	if _, err := bindings.Save(context.Background(), domain.GCloudResourceBinding{
		RepositoryID: repoID,
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
	}); err != nil {
		t.Fatal(err)
	}
	svc := newService(t, &fakeCredentialStore{}, bindings, &fakeClient{})

	bound, err := svc.Resource(context.Background(), repoID, "")
	if !errors.Is(err, gcloudops.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
	if bound.Binding.ResourceName == "" {
		t.Fatal("the binding was dropped along with the live view")
	}
	if bound.Detail != nil {
		t.Fatal("a detail was reported with no credential behind it")
	}
}

func TestResourceReadsTheLiveDetail(t *testing.T) {
	bindings := newFakeBindingStore()
	repoID := uuid.New()
	if _, err := bindings.Save(context.Background(), domain.GCloudResourceBinding{
		RepositoryID: repoID,
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{runDetail: domain.CloudRunServiceDetail{
		Ref:                 domain.GCloudResourceRef{Type: domain.GCloudResourceCloudRun, Name: "projects/demo-project/locations/us-central1/services/api", URI: "https://api.run.app"},
		LatestReadyRevision: "api-00007-xyz",
		Image:               "gcr.io/demo/api:v1",
		Ready:               "CONDITION_SUCCEEDED",
		Traffic:             []domain.CloudRunTrafficTarget{{Revision: "api-00007-xyz", Percent: 100}},
	}}
	svc := connectedService(t, client, bindings)

	bound, err := svc.Resource(context.Background(), repoID, "")
	if err != nil {
		t.Fatalf("Resource: %v", err)
	}
	if bound.Detail == nil || bound.Detail.CloudRun == nil {
		t.Fatal("no cloud run detail was returned")
	}
	if bound.Detail.GKECluster != nil {
		t.Fatal("both arms of the union were set")
	}
	if bound.Detail.CloudRun.LatestReadyRevision != "api-00007-xyz" || bound.Detail.CloudRun.Image != "gcr.io/demo/api:v1" {
		t.Fatalf("detail = %+v", bound.Detail.CloudRun)
	}
}

func TestResourceReportsAnAbsentBindingAsNotFound(t *testing.T) {
	svc := connectedService(t, &fakeClient{}, newFakeBindingStore())
	_, err := svc.Resource(context.Background(), uuid.New(), "")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want port.ErrNotFound", err)
	}
}

// TestDeleteCredentialKeepsTheBindings: a rotated key must not force someone
// to re-pick every service.
func TestDeleteCredentialKeepsTheBindings(t *testing.T) {
	bindings := newFakeBindingStore()
	repoID := uuid.New()
	if _, err := bindings.Save(context.Background(), domain.GCloudResourceBinding{
		RepositoryID: repoID,
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
	}); err != nil {
		t.Fatal(err)
	}
	svc := connectedService(t, &fakeClient{}, bindings)

	if err := svc.DeleteCredential(context.Background()); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if len(bindings.rows) != 1 {
		t.Fatal("disconnecting the credential erased the bindings")
	}
}
