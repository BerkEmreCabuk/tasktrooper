package gcloud_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/gcloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// serviceAccountJSON builds a syntactically real service account key file
// around a freshly generated RSA key, so the JWT the client mints is actually
// signable. The key never leaves the test process.
func serviceAccountJSON(t *testing.T, tokenURL string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	raw, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   "demo-project",
		"client_email": "tasktrooper@demo-project.iam.gserviceaccount.com",
		"private_key":  string(pemBytes),
		"token_uri":    tokenURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// tokenServer is a stand-in for Google's OAuth token endpoint. It counts
// exchanges so the caching test can prove a second API call does not mint a
// second token.
type tokenServer struct {
	*httptest.Server
	mu        sync.Mutex
	exchanges int
	status    int
}

func newTokenServer(t *testing.T) *tokenServer {
	t.Helper()
	ts := &tokenServer{status: http.StatusOK}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.exchanges++
		status := ts.status
		ts.mu.Unlock()

		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("token request body: %v", err)
		}
		if got := r.PostFormValue("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q, want the jwt-bearer grant", got)
		}
		// Three dot-separated base64 segments is the shape of a signed JWT;
		// an assertion that never got signed would not have the third.
		if parts := strings.Split(r.PostFormValue("assertion"), "."); len(parts) != 3 {
			t.Errorf("assertion has %d segments, want 3", len(parts))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.test-token","expires_in":3600}`))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *tokenServer) count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.exchanges
}

// newClient builds a Client pointed at the three fakes.
func newClient(t *testing.T, tokenURL, runURL, containerURL string) *gcloud.Client {
	t.Helper()
	c, err := gcloud.New(domain.GCloudCredential{
		Data: map[string]string{"service_account_json": serviceAccountJSON(t, tokenURL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.SetTokenURL(tokenURL)
	c.SetRunBaseURL(runURL)
	c.SetContainerBaseURL(containerURL)
	return c
}

func TestNewRejectsAMissingServiceAccount(t *testing.T) {
	if _, err := gcloud.New(domain.GCloudCredential{Data: map[string]string{}}); err == nil {
		t.Fatal("expected an error for a credential with no service_account_json")
	}
}

// TestNewNeverLeaksKeyMaterialIntoErrors is the security invariant of this
// package: a malformed key file must not put any part of itself into an error
// string, because errors are logged.
func TestNewNeverLeaksKeyMaterialIntoErrors(t *testing.T) {
	secret := "-----BEGIN PRIVATE KEY-----\nSUPERSECRETKEYBYTES\n-----END PRIVATE KEY-----\n"
	raw, err := json.Marshal(map[string]string{
		"client_email": "sa@demo.iam.gserviceaccount.com",
		"private_key":  secret,
		"project_id":   "demo-project",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = gcloud.New(domain.GCloudCredential{Data: map[string]string{"service_account_json": string(raw)}})
	if err == nil {
		t.Fatal("expected an error for an unparseable private key")
	}
	if strings.Contains(err.Error(), "SUPERSECRETKEYBYTES") || strings.Contains(err.Error(), "BEGIN PRIVATE KEY") {
		t.Fatalf("error leaked key material: %s", err)
	}
}

func TestValidateAuthExchangesTheAssertionForAToken(t *testing.T) {
	tok := newTokenServer(t)
	c := newClient(t, tok.URL, "http://unused", "http://unused")

	if err := c.ValidateAuth(context.Background()); err != nil {
		t.Fatalf("ValidateAuth: %v", err)
	}
	if tok.count() != 1 {
		t.Fatalf("token exchanges = %d, want 1", tok.count())
	}
}

func TestValidateAuthReportsARejectedAssertion(t *testing.T) {
	tok := newTokenServer(t)
	tok.status = http.StatusBadRequest
	c := newClient(t, tok.URL, "http://unused", "http://unused")

	if err := c.ValidateAuth(context.Background()); err == nil {
		t.Fatal("expected an error when the token endpoint refuses the assertion")
	}
}

// TestListCloudRunServicesUsesTheAggregatedCall proves the happy path is ONE
// request covering every region, not a fan-out.
func TestListCloudRunServicesUsesTheAggregatedCall(t *testing.T) {
	tok := newTokenServer(t)

	var paths []string
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer ya29.test-token" {
			t.Errorf("Authorization = %q, want the minted bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"services":[
			{"name":"projects/demo-project/locations/europe-west1/services/api","uri":"https://api.run.app","terminalCondition":{"state":"CONDITION_SUCCEEDED"}},
			{"name":"projects/demo-project/locations/us-central1/services/worker","uri":"https://worker.run.app","terminalCondition":{"state":"CONDITION_FAILED"}}
		]}`))
	}))
	defer run.Close()

	c := newClient(t, tok.URL, run.URL, "http://unused")
	list, err := c.ListCloudRunServices(context.Background())
	if err != nil {
		t.Fatalf("ListCloudRunServices: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("issued %d requests (%v), want 1 aggregated call", len(paths), paths)
	}
	if !strings.Contains(paths[0], "/locations/-/services") {
		t.Fatalf("aggregated call went to %q, want the locations/- wildcard", paths[0])
	}
	if len(list.Resources) != 2 {
		t.Fatalf("got %d services, want 2", len(list.Resources))
	}
	// Sorted by fully qualified name, so europe-west1 precedes us-central1.
	first := list.Resources[0]
	if first.Type != domain.GCloudResourceCloudRun {
		t.Fatalf("type = %q, want %q", first.Type, domain.GCloudResourceCloudRun)
	}
	if first.DisplayName != "api" || first.Location != "europe-west1" || first.ProjectID != "demo-project" {
		t.Fatalf("ref not decomposed from the qualified name: %+v", first)
	}
	if first.State != "CONDITION_SUCCEEDED" {
		t.Fatalf("state = %q, want the terminal condition passed through", first.State)
	}
	// The token was minted once and reused across both the exchange and the
	// listing call.
	if tok.count() != 1 {
		t.Fatalf("token exchanges = %d, want 1", tok.count())
	}
}

// TestListCloudRunServicesFallsBackToPerRegion is the region-traversal
// contract: when Google rejects the locations/- wildcard as a malformed
// parent, the client enumerates the project's Cloud Run locations and queries
// each one instead of reporting an empty project.
func TestListCloudRunServicesFallsBackToPerRegion(t *testing.T) {
	tok := newTokenServer(t)

	var mu sync.Mutex
	queried := map[string]bool{}
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/locations/-/services"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Location '-' is not a valid location"}}`))
		case strings.HasSuffix(r.URL.Path, "/locations"):
			_, _ = w.Write([]byte(`{"locations":[{"locationId":"europe-west1"},{"locationId":"us-central1"},{"locationId":"asia-east1"}]}`))
		case strings.Contains(r.URL.Path, "/locations/asia-east1/services"):
			// One region failing must be reported, not silently dropped.
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":500}}`))
		default:
			parts := strings.Split(r.URL.Path, "/")
			loc := parts[len(parts)-2]
			mu.Lock()
			queried[loc] = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"services":[{"name":"projects/demo-project/locations/` + loc + `/services/svc-` + loc + `"}]}`))
		}
	}))
	defer run.Close()

	c := newClient(t, tok.URL, run.URL, "http://unused")
	list, err := c.ListCloudRunServices(context.Background())
	if err != nil {
		t.Fatalf("ListCloudRunServices: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !queried["europe-west1"] || !queried["us-central1"] {
		t.Fatalf("fallback did not query every location: %v", queried)
	}
	if len(list.Resources) != 2 {
		t.Fatalf("got %d services, want 2 (the third region failed)", len(list.Resources))
	}
	if len(list.UnreachableLocations) != 1 || list.UnreachableLocations[0] != "asia-east1" {
		t.Fatalf("unreachable = %v, want [asia-east1] — a failed region must not read as empty", list.UnreachableLocations)
	}
}

// TestListCloudRunServicesReportsAForbiddenProjectAsUnavailable is the
// "connected but cannot list" branch: a 403 is an ungranted IAM role, not a
// failure, and must arrive as the sentinel so the edge answers 200.
func TestListCloudRunServicesReportsAForbiddenProjectAsUnavailable(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Permission 'run.services.list' denied"}}`))
	}))
	defer run.Close()

	c := newClient(t, tok.URL, run.URL, "http://unused")
	_, err := c.ListCloudRunServices(context.Background())
	if !errors.Is(err, port.ErrGCloudListingUnavailable) {
		t.Fatalf("err = %v, want it to wrap ErrGCloudListingUnavailable", err)
	}
}

// TestListCloudRunServicesKeeps401AsARealFailure guards the line between the
// two: a broken credential must NOT be reported as "cannot enumerate", or an
// operator goes to the IAM console over an expired key.
func TestListCloudRunServicesKeeps401AsARealFailure(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer run.Close()

	c := newClient(t, tok.URL, run.URL, "http://unused")
	_, err := c.ListCloudRunServices(context.Background())
	if err == nil {
		t.Fatal("expected an error for a 401")
	}
	if errors.Is(err, port.ErrGCloudListingUnavailable) {
		t.Fatalf("401 was reported as a listing-unavailable answer: %v", err)
	}
}

func TestListCloudRunServicesPagesTheCursor(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"services":[{"name":"projects/demo-project/locations/us-central1/services/a"}],"nextPageToken":"page2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"services":[{"name":"projects/demo-project/locations/us-central1/services/b"}]}`))
	}))
	defer run.Close()

	c := newClient(t, tok.URL, run.URL, "http://unused")
	list, err := c.ListCloudRunServices(context.Background())
	if err != nil {
		t.Fatalf("ListCloudRunServices: %v", err)
	}
	if len(list.Resources) != 2 {
		t.Fatalf("got %d services, want both pages", len(list.Resources))
	}
}

func TestCloudRunServiceReadsTheDetail(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/projects/demo-project/locations/us-central1/services/api" {
			t.Errorf("detail path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name":"projects/demo-project/locations/us-central1/services/api",
			"uri":"https://api-abc.a.run.app",
			"latestReadyRevision":"projects/demo-project/locations/us-central1/services/api/revisions/api-00007-xyz",
			"latestCreatedRevision":"projects/demo-project/locations/us-central1/services/api/revisions/api-00008-abc",
			"updateTime":"2026-08-30T10:11:12Z",
			"template":{"containers":[{"image":"europe-docker.pkg.dev/demo-project/apps/api:v42"}]},
			"trafficStatuses":[
				{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION","revision":"projects/demo-project/locations/us-central1/services/api/revisions/api-00007-xyz","percent":80},
				{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":20,"tag":"canary","uri":"https://canary---api-abc.a.run.app"}
			],
			"terminalCondition":{"type":"Ready","state":"CONDITION_FAILED","reason":"RevisionFailed","message":"the revision did not become ready"}
		}`))
	}))
	defer run.Close()

	c := newClient(t, tok.URL, run.URL, "http://unused")
	detail, err := c.CloudRunService(context.Background(), "projects/demo-project/locations/us-central1/services/api")
	if err != nil {
		t.Fatalf("CloudRunService: %v", err)
	}

	if detail.Ref.URI != "https://api-abc.a.run.app" {
		t.Fatalf("service url = %q", detail.Ref.URI)
	}
	if detail.LatestReadyRevision != "api-00007-xyz" || detail.LatestCreatedRevision != "api-00008-abc" {
		t.Fatalf("revisions = %q / %q, want the short names", detail.LatestReadyRevision, detail.LatestCreatedRevision)
	}
	if detail.Image != "europe-docker.pkg.dev/demo-project/apps/api:v42" {
		t.Fatalf("image = %q", detail.Image)
	}
	if detail.Ready != "CONDITION_FAILED" || detail.ReadyReason != "RevisionFailed" {
		t.Fatalf("readiness = %q / %q", detail.Ready, detail.ReadyReason)
	}
	if len(detail.Traffic) != 2 {
		t.Fatalf("traffic targets = %d, want 2", len(detail.Traffic))
	}
	if detail.Traffic[0].Revision != "api-00007-xyz" || detail.Traffic[0].Percent != 80 {
		t.Fatalf("first traffic target = %+v", detail.Traffic[0])
	}
	// A LATEST target names no revision of its own; it must resolve to the
	// latest ready one rather than render as a blank row.
	if detail.Traffic[1].Revision != "api-00007-xyz" || detail.Traffic[1].Tag != "canary" {
		t.Fatalf("latest traffic target = %+v", detail.Traffic[1])
	}
	if detail.UpdateTime.IsZero() {
		t.Fatal("update time was not parsed")
	}
}

func TestCloudRunServiceReportsAMissingServiceAsNotFound(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer run.Close()

	c := newClient(t, tok.URL, run.URL, "http://unused")
	_, err := c.CloudRunService(context.Background(), "projects/demo-project/locations/us-central1/services/gone")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want it to wrap port.ErrNotFound", err)
	}
}

func TestCloudRunServiceRejectsAShortName(t *testing.T) {
	tok := newTokenServer(t)
	c := newClient(t, tok.URL, "http://unused", "http://unused")
	_, err := c.CloudRunService(context.Background(), "api")
	if !errors.Is(err, domain.ErrInvalidGCloudResource) {
		t.Fatalf("err = %v, want it to wrap ErrInvalidGCloudResource", err)
	}
}

func TestListGKEClustersUsesTheWildcardAndReportsMissingZones(t *testing.T) {
	tok := newTokenServer(t)
	container := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/demo-project/locations/-/clusters" {
			t.Errorf("cluster list path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"clusters":[{"name":"prod","location":"europe-west1","status":"RUNNING","endpoint":"34.1.2.3","currentMasterVersion":"1.29.4-gke.1","currentNodeCount":6}],
			"missingZones":["us-east1-b"]
		}`))
	}))
	defer container.Close()

	c := newClient(t, tok.URL, "http://unused", container.URL)
	list, err := c.ListGKEClusters(context.Background())
	if err != nil {
		t.Fatalf("ListGKEClusters: %v", err)
	}
	if len(list.Resources) != 1 {
		t.Fatalf("got %d clusters, want 1", len(list.Resources))
	}
	ref := list.Resources[0]
	if ref.Type != domain.GCloudResourceGKECluster {
		t.Fatalf("type = %q", ref.Type)
	}
	// The Container API returns the SHORT name; the qualified one is built
	// here and is what a binding stores.
	if ref.Name != "projects/demo-project/locations/europe-west1/clusters/prod" {
		t.Fatalf("name = %q, want the fully qualified form", ref.Name)
	}
	if ref.State != "RUNNING" || ref.URI != "34.1.2.3" {
		t.Fatalf("ref = %+v", ref)
	}
	if len(list.UnreachableLocations) != 1 || list.UnreachableLocations[0] != "us-east1-b" {
		t.Fatalf("missingZones were not surfaced: %v", list.UnreachableLocations)
	}
}

func TestListGKEClustersReportsAForbiddenProjectAsUnavailable(t *testing.T) {
	tok := newTokenServer(t)
	container := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer container.Close()

	c := newClient(t, tok.URL, "http://unused", container.URL)
	_, err := c.ListGKEClusters(context.Background())
	if !errors.Is(err, port.ErrGCloudListingUnavailable) {
		t.Fatalf("err = %v, want it to wrap ErrGCloudListingUnavailable", err)
	}
}

// TestGKEClusterStatesThatWorkloadsAreNotListed is the honesty test for the
// scope limit: the detail must carry workloads_available:false with a reason,
// never an empty workload list that reads as "this cluster runs nothing".
func TestGKEClusterStatesThatWorkloadsAreNotListed(t *testing.T) {
	tok := newTokenServer(t)
	container := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/demo-project/locations/europe-west1/clusters/prod" {
			t.Errorf("cluster get path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name":"prod","location":"europe-west1","status":"RUNNING","statusMessage":"",
			"currentMasterVersion":"1.29.4-gke.1","currentNodeCount":6,"endpoint":"34.1.2.3",
			"nodePools":[{"name":"default-pool","status":"RUNNING","initialNodeCount":3,"version":"1.29.4-gke.1","config":{"machineType":"e2-standard-4"}}],
			"privateClusterConfig":{"enablePrivateEndpoint":true}
		}`))
	}))
	defer container.Close()

	c := newClient(t, tok.URL, "http://unused", container.URL)
	detail, err := c.GKECluster(context.Background(), "projects/demo-project/locations/europe-west1/clusters/prod")
	if err != nil {
		t.Fatalf("GKECluster: %v", err)
	}

	if detail.Status != "RUNNING" || detail.MasterVersion != "1.29.4-gke.1" || detail.NodeCount != 6 {
		t.Fatalf("cluster basics = %+v", detail)
	}
	if len(detail.NodePools) != 1 || detail.NodePools[0].MachineType != "e2-standard-4" {
		t.Fatalf("node pools = %+v", detail.NodePools)
	}
	if !detail.PrivateEndpoint {
		t.Fatal("private endpoint was not detected")
	}
	if detail.WorkloadsAvailable {
		t.Fatal("WorkloadsAvailable is true — this integration does not read a cluster's Kubernetes API")
	}
	if detail.WorkloadsNote == "" {
		t.Fatal("WorkloadsNote is empty — an unexplained false reads as 'no workloads'")
	}
	if !strings.Contains(detail.WorkloadsNote, "endpoint") {
		t.Fatalf("a private cluster's note does not mention reachability: %q", detail.WorkloadsNote)
	}
}

func TestGKEClusterRejectsACloudRunName(t *testing.T) {
	tok := newTokenServer(t)
	c := newClient(t, tok.URL, "http://unused", "http://unused")
	_, err := c.GKECluster(context.Background(), "projects/demo-project/locations/us-central1/services/api")
	if !errors.Is(err, domain.ErrInvalidGCloudResource) {
		t.Fatalf("err = %v, want it to wrap ErrInvalidGCloudResource", err)
	}
}

func TestIdentityReportsTheServiceAccountAndProject(t *testing.T) {
	tok := newTokenServer(t)
	c := newClient(t, tok.URL, "http://unused", "http://unused")
	id := c.Identity()
	if id.ProjectID != "demo-project" {
		t.Fatalf("project = %q", id.ProjectID)
	}
	if id.ClientEmail != "tasktrooper@demo-project.iam.gserviceaccount.com" {
		t.Fatalf("client email = %q", id.ClientEmail)
	}
}

// TestExplicitProjectIDOverridesTheKeyFile covers the shared-service-account
// case: one key file, roles granted on a different project.
func TestExplicitProjectIDOverridesTheKeyFile(t *testing.T) {
	tok := newTokenServer(t)
	c, err := gcloud.New(domain.GCloudCredential{
		ProjectID: "other-project",
		Data:      map[string]string{"service_account_json": serviceAccountJSON(t, tok.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Identity().ProjectID; got != "other-project" {
		t.Fatalf("project = %q, want the explicit one", got)
	}
}
