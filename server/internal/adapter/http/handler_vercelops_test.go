package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	vercelapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vercel"
	"github.com/makifbaysal/tasktrooper/server/internal/application/vercelops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// --- fakes ------------------------------------------------------------------

type fakeVercelLinks struct {
	rows map[string]domain.VercelProjectLink
}

func newFakeVercelLinks() *fakeVercelLinks {
	return &fakeVercelLinks{rows: map[string]domain.VercelProjectLink{}}
}

func (f *fakeVercelLinks) ListByRepository(context.Context, uuid.UUID) ([]domain.VercelProjectLink, error) {
	out := make([]domain.VercelProjectLink, 0, len(f.rows))
	for _, l := range f.rows {
		out = append(out, l)
	}
	return out, nil
}

func (f *fakeVercelLinks) Get(_ context.Context, _ uuid.UUID, path string) (domain.VercelProjectLink, error) {
	l, ok := f.rows[path]
	if !ok {
		return domain.VercelProjectLink{}, port.ErrNotFound
	}
	return l, nil
}

func (f *fakeVercelLinks) Save(_ context.Context, l domain.VercelProjectLink) (domain.VercelProjectLink, error) {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	f.rows[l.SubProjectPath] = l
	return l, nil
}

func (f *fakeVercelLinks) Delete(_ context.Context, _ uuid.UUID, path string) error {
	delete(f.rows, path)
	return nil
}

type fakeVercelCreds struct{ token, team string }

func (f *fakeVercelCreds) VercelToken(context.Context) (string, error)  { return f.token, nil }
func (f *fakeVercelCreds) SetVercelToken(context.Context, string) error { return nil }
func (f *fakeVercelCreds) DeleteVercelToken(context.Context) error      { return nil }
func (f *fakeVercelCreds) VercelTeam(context.Context) (string, error)   { return f.team, nil }
func (f *fakeVercelCreds) SetVercelTeam(context.Context, string) error  { return nil }

type fakeVercelHTTPAPI struct {
	projects map[string][]domain.VercelProject
	byScope  map[string]map[string]domain.VercelProject
	scopeErr map[string]error
	teams    []domain.VercelTeam
	teamsErr error
}

func (f *fakeVercelHTTPAPI) User(context.Context, string) (domain.VercelUser, error) {
	return domain.VercelUser{Username: "operator"}, nil
}

func (f *fakeVercelHTTPAPI) Teams(context.Context, string) ([]domain.VercelTeam, error) {
	return f.teams, f.teamsErr
}

func (f *fakeVercelHTTPAPI) Projects(_ context.Context, _, teamID string) ([]domain.VercelProject, error) {
	if err, ok := f.scopeErr[teamID]; ok {
		return nil, err
	}
	return f.projects[teamID], nil
}

func (f *fakeVercelHTTPAPI) Project(_ context.Context, _, teamID, id string) (domain.VercelProject, error) {
	if err, ok := f.scopeErr[teamID]; ok {
		return domain.VercelProject{}, err
	}
	p, ok := f.byScope[teamID][id]
	if !ok {
		return domain.VercelProject{}, &vercelapi.APIError{Status: http.StatusNotFound, Message: "not found"}
	}
	return p, nil
}

type fakeVercelDeployments struct {
	byTarget map[string][]domain.VercelDeployment
	err      error
}

func (f *fakeVercelDeployments) Deployments(_ context.Context, _, _, _, target string, _ int) ([]domain.VercelDeployment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byTarget[target], nil
}

type fakeVercelRepos struct{ repo domain.Repository }

func (f *fakeVercelRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

func vercelOpsApp(svc *vercelops.Service) *fiber.App {
	h := &Handler{vercelOpsSvc: svc}
	app := fiber.New()
	h.registerVercelOpsRoutes(app)
	return app
}

func decodeListing(t *testing.T, resp *http.Response) vercelProjectListing {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out vercelProjectListing
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return out
}

// --- listing ----------------------------------------------------------------

// TestListVercelProjectsAnswers200NotConnected pins the contract the console
// branches on: an unconnected account is a 200 carrying the reason, never a
// 5xx. A 5xx would tell the operator to retry something that cannot start
// working until they paste a token.
func TestListVercelProjectsAnswers200NotConnected(t *testing.T) {
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeVercelLinks(), Creds: &fakeVercelCreds{}, API: &fakeVercelHTTPAPI{},
	})
	app := vercelOpsApp(svc)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/vercel/projects", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	listing := decodeListing(t, resp)
	if listing.Available {
		t.Fatal("listing_available must be false with no token stored")
	}
	if listing.Reason != listingReasonNotConnected {
		t.Fatalf("reason = %q, want %q", listing.Reason, listingReasonNotConnected)
	}
	if listing.Projects == nil {
		t.Fatal("projects must be an empty array, never null — the picker iterates it")
	}
}

// TestListVercelProjectsAnswers200ListingUnsupported is the other half of the
// split: a token IS stored and Vercel refuses it. Same 200, different reason,
// because the console sends the operator somewhere different for each.
func TestListVercelProjectsAnswers200ListingUnsupported(t *testing.T) {
	refused := &vercelapi.APIError{Status: http.StatusForbidden, Message: "not authorized"}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeVercelLinks(),
		Creds: &fakeVercelCreds{token: "tok"},
		API:   &fakeVercelHTTPAPI{scopeErr: map[string]error{"": refused}, teamsErr: refused},
	})
	app := vercelOpsApp(svc)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/vercel/projects", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 — a refused token is an answer, not a server failure", resp.StatusCode)
	}
	listing := decodeListing(t, resp)
	if listing.Available {
		t.Fatal("listing_available must be false when the token is refused")
	}
	if listing.Reason != listingReasonUnsupported {
		t.Fatalf("reason = %q, want %q", listing.Reason, listingReasonUnsupported)
	}
	if listing.Reason == listingReasonNotConnected {
		t.Fatal("a stored-but-refused token must not report not_connected")
	}
}

// TestListVercelProjectsKeeps500ForARealFailure: the two reasons must not
// swallow an outage, or a genuine incident would render as a normal empty
// picker forever.
func TestListVercelProjectsKeeps500ForARealFailure(t *testing.T) {
	boom := &vercelapi.APIError{Status: http.StatusBadGateway, Message: "upstream"}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeVercelLinks(),
		Creds: &fakeVercelCreds{token: "tok"},
		API:   &fakeVercelHTTPAPI{scopeErr: map[string]error{"": boom}, teamsErr: boom},
	})
	app := vercelOpsApp(svc)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/vercel/projects", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

// TestListVercelProjectsReturnsThePickerRows is the happy path end to end.
func TestListVercelProjectsReturnsThePickerRows(t *testing.T) {
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeVercelLinks(),
		Creds: &fakeVercelCreds{token: "tok"},
		API: &fakeVercelHTTPAPI{
			teams:    []domain.VercelTeam{{ID: "team_1", Slug: "acme"}},
			projects: map[string][]domain.VercelProject{"team_1": {{ID: "prj_1", Name: "acme-web"}}},
		},
	})
	app := vercelOpsApp(svc)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/vercel/projects", nil))
	if err != nil {
		t.Fatal(err)
	}
	listing := decodeListing(t, resp)
	if !listing.Available || listing.Reason != "" {
		t.Fatalf("a working listing states available with no reason: %+v", listing)
	}
	if len(listing.Projects) != 1 || listing.Projects[0].ID != "prj_1" {
		t.Fatalf("projects = %+v, want prj_1", listing.Projects)
	}
}

// --- linking ----------------------------------------------------------------

// TestLinkVercelProjectRejectsAnUnreachableProjectWith400 proves the refusal
// reaches the caller as their own mistake, not as a server failure.
func TestLinkVercelProjectRejectsAnUnreachableProjectWith400(t *testing.T) {
	links := newFakeVercelLinks()
	repoID := uuid.New()
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: &fakeVercelCreds{token: "tok"},
		API:   &fakeVercelHTTPAPI{byScope: map[string]map[string]domain.VercelProject{}},
		Repos: &fakeVercelRepos{repo: domain.Repository{ID: repoID, Name: "site"}},
	})
	app := vercelOpsApp(svc)

	req := httptest.NewRequest("PUT", "/v1/repositories/"+repoID.String()+"/vercel/project",
		strings.NewReader(`{"project_id":"prj_gone"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(links.rows) != 0 {
		t.Fatalf("a refused link must persist nothing, got %v", links.rows)
	}
}

// TestLinkVercelProjectStoresTheSubProjectPath is the whole reason this route
// exists beside the area-keyed hosting link: two frontends, two projects.
func TestLinkVercelProjectStoresTheSubProjectPath(t *testing.T) {
	links := newFakeVercelLinks()
	repoID := uuid.New()
	repo := domain.Repository{ID: repoID, Name: "monorepo", Kind: domain.RepoKindMonorepo,
		SubProjects: []domain.RepoSubProject{
			{Path: "web", Kind: domain.RepoKindFrontend},
			{Path: "admin", Kind: domain.RepoKindFrontend},
		}}
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: &fakeVercelCreds{token: "tok"},
		API: &fakeVercelHTTPAPI{byScope: map[string]map[string]domain.VercelProject{
			"": {
				"prj_web":   {ID: "prj_web", Name: "acme-web"},
				"prj_admin": {ID: "prj_admin", Name: "acme-admin"},
			},
		}},
		Repos: &fakeVercelRepos{repo: repo},
	})
	app := vercelOpsApp(svc)

	for path, projectID := range map[string]string{"web": "prj_web", "admin": "prj_admin"} {
		req := httptest.NewRequest("PUT", "/v1/repositories/"+repoID.String()+"/vercel/project",
			strings.NewReader(`{"project_id":"`+projectID+`","sub_project_path":"`+path+`"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, resp.StatusCode)
		}
	}
	if len(links.rows) != 2 {
		t.Fatalf("two sub-projects must produce two links, got %v", links.rows)
	}
	if links.rows["web"].ProjectID != "prj_web" || links.rows["admin"].ProjectID != "prj_admin" {
		t.Fatalf("the two links crossed over: %v", links.rows)
	}
}

// TestLinkVercelProjectRejectsInvalidRepositoryID covers the uuid.Parse guard.
func TestLinkVercelProjectRejectsInvalidRepositoryID(t *testing.T) {
	svc := vercelops.NewService(vercelops.Deps{Links: newFakeVercelLinks(), Creds: &fakeVercelCreds{}})
	app := vercelOpsApp(svc)

	req := httptest.NewRequest("PUT", "/v1/repositories/not-a-uuid/vercel/project",
		strings.NewReader(`{"project_id":"prj_1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestLinkVercelProjectAnswers409WhenVercelIsNotConnected: a configuration gap
// the caller can go fix, matching hostingError on the neighbouring surface —
// and specifically not a 400, which would say their request was malformed.
func TestLinkVercelProjectAnswers409WhenVercelIsNotConnected(t *testing.T) {
	repoID := uuid.New()
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeVercelLinks(), Creds: &fakeVercelCreds{}, API: &fakeVercelHTTPAPI{},
		Repos: &fakeVercelRepos{repo: domain.Repository{ID: repoID}},
	})
	app := vercelOpsApp(svc)

	req := httptest.NewRequest("PUT", "/v1/repositories/"+repoID.String()+"/vercel/project",
		strings.NewReader(`{"project_id":"prj_1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

// --- details ----------------------------------------------------------------

// TestVercelProjectDetailsReturns200WithPartialData is the partial-read guard
// at the HTTP edge: Vercel unreachable must still render the link, with the
// failures reported as warnings rather than as an error page.
func TestVercelProjectDetailsReturns200WithPartialData(t *testing.T) {
	boom := &vercelapi.APIError{Status: http.StatusBadGateway, Message: "upstream"}
	links := newFakeVercelLinks()
	repoID := uuid.New()
	links.rows["web"] = domain.VercelProjectLink{
		RepositoryID: repoID, SubProjectPath: "web", ProjectID: "prj_1",
		ProjectName: "acme-web", ProductionURL: "https://recorded.example",
	}
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: &fakeVercelCreds{token: "tok"},
		API:         &fakeVercelHTTPAPI{scopeErr: map[string]error{"": boom}},
		Deployments: &fakeVercelDeployments{err: boom},
		Repos:       &fakeVercelRepos{repo: domain.Repository{ID: repoID}},
	})
	app := vercelOpsApp(svc)

	resp, err := app.Test(httptest.NewRequest("GET",
		"/v1/repositories/"+repoID.String()+"/vercel/project?sub_project_path=web", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 — the link row is durable and worth rendering", resp.StatusCode)
	}
	var details domain.VercelProjectDetails
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	if details.Link.ProjectID != "prj_1" || details.ProductionURL != "https://recorded.example" {
		t.Fatalf("the recorded values must survive: %s", raw)
	}
	if details.LatestDeployment != nil {
		t.Fatal("nothing could be read, so no deployment may be reported")
	}
	if len(details.Warnings) == 0 {
		t.Fatalf("the failed reads must be reported as warnings: %s", raw)
	}
}

// TestVercelProjectDetailsReturns404WithoutALink: the one case that IS an
// error, so the console can offer the picker instead of an empty panel.
func TestVercelProjectDetailsReturns404WithoutALink(t *testing.T) {
	repoID := uuid.New()
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeVercelLinks(), Creds: &fakeVercelCreds{token: "tok"},
		API: &fakeVercelHTTPAPI{}, Repos: &fakeVercelRepos{repo: domain.Repository{ID: repoID}},
	})
	app := vercelOpsApp(svc)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/repositories/"+repoID.String()+"/vercel/project", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestRegisterVercelOpsRoutesNoopWithoutService matches the graceful-degrade
// pattern every other opt-in route group follows: no service, no routes.
func TestRegisterVercelOpsRoutesNoopWithoutService(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerVercelOpsRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/vercel/projects", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route must not be registered)", resp.StatusCode)
	}
}
