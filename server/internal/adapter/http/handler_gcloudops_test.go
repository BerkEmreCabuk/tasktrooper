package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/gcloudops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeGCloudCredentialStore stores whatever bytes it is handed untouched, so a
// test can seed a raw row and prove the HTTP layer never echoes stored bytes
// back — only the safe view.
type fakeGCloudCredentialStore struct {
	row     port.GCloudCredentialRow
	present bool
}

var _ port.GCloudCredentialStore = (*fakeGCloudCredentialStore)(nil)

func (f *fakeGCloudCredentialStore) Set(_ context.Context, projectID, clientEmail string, encrypted []byte) error {
	f.row = port.GCloudCredentialRow{ProjectID: projectID, ClientEmail: clientEmail, Data: encrypted, UpdatedAt: time.Now()}
	f.present = true
	return nil
}

func (f *fakeGCloudCredentialStore) Get(_ context.Context) (port.GCloudCredentialRow, error) {
	if !f.present {
		return port.GCloudCredentialRow{}, port.ErrNotFound
	}
	return f.row, nil
}

func (f *fakeGCloudCredentialStore) Delete(_ context.Context) error {
	f.present = false
	return nil
}

// fakeGCloudBindingStore is an in-memory port.GCloudResourceStore.
type fakeGCloudBindingStore struct {
	rows map[string]domain.GCloudResourceBinding
}

var _ port.GCloudResourceStore = (*fakeGCloudBindingStore)(nil)

func newFakeGCloudBindingStore() *fakeGCloudBindingStore {
	return &fakeGCloudBindingStore{rows: map[string]domain.GCloudResourceBinding{}}
}

func gcloudBindingKey(repositoryID uuid.UUID, path string) string {
	return repositoryID.String() + "/" + path
}

func (f *fakeGCloudBindingStore) Save(_ context.Context, b domain.GCloudResourceBinding) (domain.GCloudResourceBinding, error) {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	f.rows[gcloudBindingKey(b.RepositoryID, b.SubProjectPath)] = b
	return b, nil
}

func (f *fakeGCloudBindingStore) Get(_ context.Context, repositoryID uuid.UUID, path string) (domain.GCloudResourceBinding, error) {
	b, ok := f.rows[gcloudBindingKey(repositoryID, path)]
	if !ok {
		return domain.GCloudResourceBinding{}, port.ErrNotFound
	}
	return b, nil
}

func (f *fakeGCloudBindingStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.GCloudResourceBinding, error) {
	var out []domain.GCloudResourceBinding
	for _, b := range f.rows {
		if b.RepositoryID == repositoryID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeGCloudBindingStore) Delete(_ context.Context, repositoryID uuid.UUID, path string) error {
	delete(f.rows, gcloudBindingKey(repositoryID, path))
	return nil
}

// fakeGCloudClient is a scriptable port.GCloudClient for the handler tests.
type fakeGCloudClient struct {
	runList   domain.GCloudResourceList
	runErr    error
	gkeList   domain.GCloudResourceList
	gkeErr    error
	runDetail domain.CloudRunServiceDetail
	getErr    error
}

var _ port.GCloudClient = (*fakeGCloudClient)(nil)

func (f *fakeGCloudClient) Identity() domain.GCloudIdentity {
	return domain.GCloudIdentity{ProjectID: "demo-project", ClientEmail: "sa@demo.iam.gserviceaccount.com"}
}
func (f *fakeGCloudClient) ValidateAuth(context.Context) error { return nil }
func (f *fakeGCloudClient) ListCloudRunServices(context.Context) (domain.GCloudResourceList, error) {
	return f.runList, f.runErr
}
func (f *fakeGCloudClient) ListGKEClusters(context.Context) (domain.GCloudResourceList, error) {
	return f.gkeList, f.gkeErr
}
func (f *fakeGCloudClient) CloudRunService(_ context.Context, name string) (domain.CloudRunServiceDetail, error) {
	if f.getErr != nil {
		return domain.CloudRunServiceDetail{}, f.getErr
	}
	d := f.runDetail
	if d.Ref.Name == "" {
		d.Ref = domain.GCloudResourceRef{Type: domain.GCloudResourceCloudRun, Name: name}
	}
	return d, nil
}
func (f *fakeGCloudClient) GKECluster(_ context.Context, name string) (domain.GKEClusterDetail, error) {
	if f.getErr != nil {
		return domain.GKEClusterDetail{}, f.getErr
	}
	return domain.GKEClusterDetail{Ref: domain.GCloudResourceRef{Type: domain.GCloudResourceGKECluster, Name: name}}, nil
}

func gcloudTestCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	c, err := secrets.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const gcloudTestKeyFile = `{"client_email":"sa@demo.iam.gserviceaccount.com","private_key":"pretend","project_id":"demo-project"}`

// newGCloudApp builds a Handler with only the gcloud service wired and returns
// the fiber app with its routes registered.
func newGCloudApp(t *testing.T, creds *fakeGCloudCredentialStore, bindings *fakeGCloudBindingStore, client *fakeGCloudClient) *fiber.App {
	t.Helper()
	svc := gcloudops.NewService(gcloudops.Deps{
		Credentials: creds,
		Bindings:    bindings,
		Cipher:      gcloudTestCipher(t),
		NewClient: func(domain.GCloudCredential) (port.GCloudClient, error) {
			if client == nil {
				return nil, errors.New("no client")
			}
			return client, nil
		},
	})
	h := &Handler{gcloudOpsSvc: svc}
	app := fiber.New()
	h.registerGCloudOpsRoutes(app)
	return app
}

// connectGCloud seeds a saved credential through the real save path, so the
// stored bytes are genuinely ciphertext.
func connectGCloud(t *testing.T, app *fiber.App) {
	t.Helper()
	req := httptest.NewRequest("PUT", "/v1/gcloud/credential", strings.NewReader(`{"data":{"service_account_json":`+strconvQuote(gcloudTestKeyFile)+`}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("seeding the credential returned %d: %s", resp.StatusCode, body)
	}
}

// strconvQuote JSON-quotes a string for embedding in a literal request body.
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestRegisterGCloudOpsRoutesNoopWithoutService mirrors the storeops guard: an
// unwired service must mount no routes rather than panic on the first request.
func TestRegisterGCloudOpsRoutesNoopWithoutService(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerGCloudOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/gcloud/credential", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route must not be registered)", resp.StatusCode)
	}
}

// TestGetGCloudCredentialNeverLeaksThePayload is the disclosure test: a stored
// key file must not appear anywhere in the response.
func TestGetGCloudCredentialNeverLeaksThePayload(t *testing.T) {
	creds := &fakeGCloudCredentialStore{}
	app := newGCloudApp(t, creds, newFakeGCloudBindingStore(), &fakeGCloudClient{})
	connectGCloud(t, app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/gcloud/credential", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	if strings.Contains(body, "private_key") || strings.Contains(body, "pretend") {
		t.Fatalf("response leaked the stored payload: %s", body)
	}
	if strings.Contains(body, `"data"`) {
		t.Fatalf("response carries a data field: %s", body)
	}
	if !strings.Contains(body, `"connected":true`) || !strings.Contains(body, "demo-project") {
		t.Fatalf("response does not name the connection: %s", body)
	}
}

func TestGetGCloudCredentialReportsDisconnected(t *testing.T) {
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), &fakeGCloudClient{})

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/gcloud/credential", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var view domain.GCloudCredentialView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Connected {
		t.Fatal("an empty vault reported itself connected")
	}
}

// TestSaveGCloudCredentialRejectsABadKeyFileWith400 proves the caller's own
// bad input reports 400, not 500.
func TestSaveGCloudCredentialRejectsABadKeyFileWith400(t *testing.T) {
	// A nil client factory result stands in for key material the adapter
	// cannot parse — the same class of failure, routed the same way.
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), nil)

	req := httptest.NewRequest("PUT", "/v1/gcloud/credential", strings.NewReader(`{"data":{"service_account_json":"not a key file"}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestListGCloudResourcesAnswersNotConnectedWith200 is the contract the store
// picker established: not connected is an answer, not a failure.
func TestListGCloudResourcesAnswersNotConnectedWith200(t *testing.T) {
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), &fakeGCloudClient{})

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/gcloud/resources", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 — a disconnected provider is not a server error", resp.StatusCode)
	}

	var got gcloudResourceListing
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Available {
		t.Fatal("listing_available is true with no credential")
	}
	if got.Reason != listingReasonNotConnected {
		t.Fatalf("reason = %q, want %q", got.Reason, listingReasonNotConnected)
	}
	if got.Resources == nil {
		t.Fatal("resources is null; it must be an empty array")
	}
}

// TestListGCloudResourcesAnswersRefusedWith200AndUnsupported is the OTHER
// answer: connected, but the credential cannot enumerate. It must be
// distinguishable from not_connected, and it must not be a 500.
func TestListGCloudResourcesAnswersRefusedWith200AndUnsupported(t *testing.T) {
	client := &fakeGCloudClient{runErr: port.ErrGCloudListingUnavailable, gkeErr: port.ErrGCloudListingUnavailable}
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), client)
	connectGCloud(t, app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/gcloud/resources", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 — a missing IAM role is not a server error", resp.StatusCode)
	}

	var got gcloudResourceListing
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Available {
		t.Fatal("listing_available is true after both families were refused")
	}
	if got.Reason != listingReasonUnsupported {
		t.Fatalf("reason = %q, want %q — this must not collapse into not_connected", got.Reason, listingReasonUnsupported)
	}
	if got.CloudRun.Reason != listingReasonUnsupported || got.GKE.Reason != listingReasonUnsupported {
		t.Fatalf("per-family reasons = %q / %q", got.CloudRun.Reason, got.GKE.Reason)
	}
}

// TestListGCloudResourcesKeepsCloudRunWhenGKEIsRefused is why the verdict is
// per-family rather than one flag.
func TestListGCloudResourcesKeepsCloudRunWhenGKEIsRefused(t *testing.T) {
	client := &fakeGCloudClient{
		runList: domain.GCloudResourceList{
			Resources:            []domain.GCloudResourceRef{{Type: domain.GCloudResourceCloudRun, Name: "projects/demo-project/locations/us-central1/services/api", DisplayName: "api"}},
			UnreachableLocations: []string{"asia-east1"},
		},
		gkeErr: port.ErrGCloudListingUnavailable,
	}
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), client)
	connectGCloud(t, app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/gcloud/resources", nil))
	if err != nil {
		t.Fatal(err)
	}
	var got gcloudResourceListing
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Available || !got.CloudRun.Available {
		t.Fatalf("cloud run should still be listable: %+v", got)
	}
	if got.GKE.Available {
		t.Fatal("gke reported available after a refusal")
	}
	if len(got.Resources) != 1 || got.Resources[0].Type != domain.GCloudResourceCloudRun {
		t.Fatalf("resources = %+v", got.Resources)
	}
	if len(got.CloudRun.UnreachableLocations) != 1 {
		t.Fatalf("unreachable locations were dropped: %+v", got.CloudRun)
	}
}

// TestListGCloudResourcesStatesThatGKEWorkloadsAreNotListed is the honesty
// contract for the scope limit.
func TestListGCloudResourcesStatesThatGKEWorkloadsAreNotListed(t *testing.T) {
	client := &fakeGCloudClient{
		gkeList: domain.GCloudResourceList{
			Resources: []domain.GCloudResourceRef{{Type: domain.GCloudResourceGKECluster, Name: "projects/demo-project/locations/europe-west1/clusters/prod"}},
		},
	}
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), client)
	connectGCloud(t, app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/gcloud/resources", nil))
	if err != nil {
		t.Fatal(err)
	}
	var got gcloudResourceListing
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.GKE.Available {
		t.Fatal("clusters should list fine")
	}
	if got.GKE.WorkloadsAvailable {
		t.Fatal("workloads_available is true — this integration does not read a cluster's Kubernetes API")
	}
	if got.GKE.WorkloadsReason != listingReasonUnsupported {
		t.Fatalf("workloads_reason = %q, want %q", got.GKE.WorkloadsReason, listingReasonUnsupported)
	}
}

func TestBindGCloudResourceStoresTheBinding(t *testing.T) {
	bindings := newFakeGCloudBindingStore()
	client := &fakeGCloudClient{runDetail: domain.CloudRunServiceDetail{
		Ref: domain.GCloudResourceRef{
			Type:        domain.GCloudResourceCloudRun,
			Name:        "projects/demo-project/locations/europe-west1/services/worker",
			DisplayName: "worker",
			ProjectID:   "demo-project",
			Location:    "europe-west1",
		},
	}}
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, bindings, client)
	connectGCloud(t, app)

	repoID := uuid.New()
	body := `{"sub_project_path":"services/worker","resource_type":"cloud_run","resource_name":"projects/demo-project/locations/europe-west1/services/worker"}`
	req := httptest.NewRequest("PUT", "/v1/repositories/"+repoID.String()+"/gcloud/resource", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, raw)
	}
	var binding domain.GCloudResourceBinding
	if err := json.NewDecoder(resp.Body).Decode(&binding); err != nil {
		t.Fatal(err)
	}
	if binding.SubProjectPath != "services/worker" || binding.Location != "europe-west1" {
		t.Fatalf("binding = %+v", binding)
	}
	if len(bindings.rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(bindings.rows))
	}
}

func TestBindGCloudResourceRejectsAShortNameWith400(t *testing.T) {
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), &fakeGCloudClient{})
	connectGCloud(t, app)

	req := httptest.NewRequest("PUT", "/v1/repositories/"+uuid.New().String()+"/gcloud/resource",
		strings.NewReader(`{"resource_type":"cloud_run","resource_name":"api"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestBindGCloudResourceReportsNoCredentialAs424 sends the console to the
// integrations screen instead of a generic error.
func TestBindGCloudResourceReportsNoCredentialAs424(t *testing.T) {
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), &fakeGCloudClient{})

	req := httptest.NewRequest("PUT", "/v1/repositories/"+uuid.New().String()+"/gcloud/resource",
		strings.NewReader(`{"resource_type":"cloud_run","resource_name":"projects/demo-project/locations/us-central1/services/api"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusFailedDependency {
		t.Fatalf("status = %d, want 424", resp.StatusCode)
	}
}

func TestGetGCloudResourceReturnsTheDetail(t *testing.T) {
	bindings := newFakeGCloudBindingStore()
	repoID := uuid.New()
	if _, err := bindings.Save(context.Background(), domain.GCloudResourceBinding{
		RepositoryID: repoID,
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeGCloudClient{runDetail: domain.CloudRunServiceDetail{
		Ref:                 domain.GCloudResourceRef{Type: domain.GCloudResourceCloudRun, Name: "projects/demo-project/locations/us-central1/services/api", URI: "https://api.run.app"},
		LatestReadyRevision: "api-00007-xyz",
		Image:               "gcr.io/demo/api:v1",
		Ready:               "CONDITION_SUCCEEDED",
		Traffic:             []domain.CloudRunTrafficTarget{{Revision: "api-00007-xyz", Percent: 100}},
	}}
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, bindings, client)
	connectGCloud(t, app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/gcloud/resource", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got boundGCloudResource
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.DetailAvailable || got.Detail == nil || got.Detail.CloudRun == nil {
		t.Fatalf("no detail returned: %+v", got)
	}
	if got.Detail.CloudRun.LatestReadyRevision != "api-00007-xyz" || got.Detail.CloudRun.Ref.URI != "https://api.run.app" {
		t.Fatalf("detail = %+v", got.Detail.CloudRun)
	}
	if len(got.Detail.CloudRun.Traffic) != 1 {
		t.Fatalf("traffic split was dropped: %+v", got.Detail.CloudRun)
	}
}

// TestGetGCloudResourceKeepsTheBindingWhenTheCredentialCannotRead: the binding
// is real state and must survive an under-privileged credential.
func TestGetGCloudResourceKeepsTheBindingWhenTheCredentialCannotRead(t *testing.T) {
	bindings := newFakeGCloudBindingStore()
	repoID := uuid.New()
	if _, err := bindings.Save(context.Background(), domain.GCloudResourceBinding{
		RepositoryID: repoID,
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
		DisplayName:  "api",
	}); err != nil {
		t.Fatal(err)
	}
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, bindings, &fakeGCloudClient{getErr: port.ErrGCloudListingUnavailable})
	connectGCloud(t, app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/gcloud/resource", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got boundGCloudResource
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.DetailAvailable {
		t.Fatal("detail_available is true after a refusal")
	}
	if got.Reason != listingReasonUnsupported {
		t.Fatalf("reason = %q, want %q", got.Reason, listingReasonUnsupported)
	}
	if got.Binding.DisplayName != "api" {
		t.Fatalf("the binding was dropped: %+v", got.Binding)
	}
}

// TestGetGCloudResourceReportsNotConnectedSeparately keeps the two answers
// apart at the edge, which is the whole point of the sentinel split.
func TestGetGCloudResourceReportsNotConnectedSeparately(t *testing.T) {
	bindings := newFakeGCloudBindingStore()
	repoID := uuid.New()
	if _, err := bindings.Save(context.Background(), domain.GCloudResourceBinding{
		RepositoryID: repoID,
		ResourceType: domain.GCloudResourceCloudRun,
		ResourceName: "projects/demo-project/locations/us-central1/services/api",
	}); err != nil {
		t.Fatal(err)
	}
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, bindings, &fakeGCloudClient{})

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/gcloud/resource", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got boundGCloudResource
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Reason != listingReasonNotConnected {
		t.Fatalf("reason = %q, want %q", got.Reason, listingReasonNotConnected)
	}
}

func TestGetGCloudResourceReportsAnAbsentBindingAs404(t *testing.T) {
	app := newGCloudApp(t, &fakeGCloudCredentialStore{}, newFakeGCloudBindingStore(), &fakeGCloudClient{})
	connectGCloud(t, app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+uuid.New().String()+"/gcloud/resource", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestGCloudCredentialRoutesAreAdminGated proves the vault sits behind the
// role middleware — the check the task asked for, made a test so a later route
// rename cannot silently drop it.
func TestGCloudCredentialRoutesAreAdminGated(t *testing.T) {
	for _, path := range []string{"/v1/gcloud/credential", "/v1/gcloud/resources"} {
		if _, ok := matchAdminRoute(path); !ok {
			t.Fatalf("%s matches no admin rule — its mutations would be open to members", path)
		}
	}
}
