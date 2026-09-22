package vercelops_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	vercelapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/vercel"
	"github.com/makifbaysal/tasktrooper/server/internal/application/vercelops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeLinks struct {
	rows      map[string]domain.VercelProjectLink
	saveErr   error
	getErr    error
	lastSaved domain.VercelProjectLink
}

func newFakeLinks() *fakeLinks {
	return &fakeLinks{rows: map[string]domain.VercelProjectLink{}}
}

func (f *fakeLinks) ListByRepository(context.Context, uuid.UUID) ([]domain.VercelProjectLink, error) {
	out := make([]domain.VercelProjectLink, 0, len(f.rows))
	for _, l := range f.rows {
		out = append(out, l)
	}
	return out, nil
}

func (f *fakeLinks) Get(_ context.Context, _ uuid.UUID, path string) (domain.VercelProjectLink, error) {
	if f.getErr != nil {
		return domain.VercelProjectLink{}, f.getErr
	}
	l, ok := f.rows[path]
	if !ok {
		return domain.VercelProjectLink{}, port.ErrNotFound
	}
	return l, nil
}

func (f *fakeLinks) Save(_ context.Context, link domain.VercelProjectLink) (domain.VercelProjectLink, error) {
	if f.saveErr != nil {
		return domain.VercelProjectLink{}, f.saveErr
	}
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	f.rows[link.SubProjectPath] = link
	f.lastSaved = link
	return link, nil
}

func (f *fakeLinks) Delete(_ context.Context, _ uuid.UUID, path string) error {
	delete(f.rows, path)
	return nil
}

type fakeCreds struct {
	token    string
	team     string
	tokenErr error
}

func (f *fakeCreds) VercelToken(context.Context) (string, error)     { return f.token, f.tokenErr }
func (f *fakeCreds) SetVercelToken(context.Context, string) error    { return nil }
func (f *fakeCreds) DeleteVercelToken(context.Context) error         { return nil }
func (f *fakeCreds) VercelTeam(context.Context) (string, error)      { return f.team, nil }
func (f *fakeCreds) SetVercelTeam(_ context.Context, s string) error { f.team = s; return nil }

type fakeAPI struct {
	user      domain.VercelUser
	userErr   error
	teams     []domain.VercelTeam
	teamsErr  error
	projects  map[string][]domain.VercelProject
	scopeErr  map[string]error
	byScope   map[string]map[string]domain.VercelProject
	projectHi int
}

func (f *fakeAPI) User(context.Context, string) (domain.VercelUser, error) {
	return f.user, f.userErr
}

func (f *fakeAPI) Teams(context.Context, string) ([]domain.VercelTeam, error) {
	return f.teams, f.teamsErr
}

func (f *fakeAPI) Projects(_ context.Context, _, teamID string) ([]domain.VercelProject, error) {
	if err, ok := f.scopeErr[teamID]; ok {
		return nil, err
	}
	return f.projects[teamID], nil
}

func (f *fakeAPI) Project(_ context.Context, _, teamID, idOrName string) (domain.VercelProject, error) {
	f.projectHi++
	if err, ok := f.scopeErr[teamID]; ok {
		return domain.VercelProject{}, err
	}
	p, ok := f.byScope[teamID][idOrName]
	if !ok {
		return domain.VercelProject{}, &vercelapi.APIError{Status: http.StatusNotFound, Message: "not found"}
	}
	return p, nil
}

type fakeDeployments struct {
	byTarget map[string][]domain.VercelDeployment
	err      error
}

func (f *fakeDeployments) Deployments(_ context.Context, _, _, _, target string, _ int) ([]domain.VercelDeployment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byTarget[target], nil
}

type fakeRepos struct {
	repo domain.Repository
	err  error
}

func (f *fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, f.err
}

func unauthorized() error {
	return &vercelapi.APIError{Status: http.StatusForbidden, Message: "not authorized"}
}

func TestListProjectsReportsNotConnectedWhenNoTokenIsStored(t *testing.T) {
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(),
		Creds: &fakeCreds{token: "   "},
		API:   &fakeAPI{},
	})

	_, err := svc.ListProjects(context.Background())
	if !errors.Is(err, vercelops.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
	if errors.Is(err, vercelops.ErrListingUnavailable) {
		t.Fatal("an unconnected account must not also report ErrListingUnavailable — the console shows a different screen for each")
	}
}

func TestListProjectsReportsListingUnavailableWhenVercelRefusesTheToken(t *testing.T) {
	api := &fakeAPI{
		scopeErr: map[string]error{"": unauthorized()},
		teamsErr: unauthorized(),
	}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(), Creds: &fakeCreds{token: "tok"}, API: api,
	})

	_, err := svc.ListProjects(context.Background())
	if !errors.Is(err, vercelops.ErrListingUnavailable) {
		t.Fatalf("err = %v, want ErrListingUnavailable", err)
	}
	if errors.Is(err, vercelops.ErrNotConnected) {
		t.Fatal("a refused token is connected — it must not report ErrNotConnected")
	}
}

func TestListProjectsKeepsAnInfrastructureFailureAsAPlainError(t *testing.T) {
	boom := &vercelapi.APIError{Status: http.StatusBadGateway, Message: "upstream"}
	api := &fakeAPI{scopeErr: map[string]error{"": boom}, teamsErr: boom}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(), Creds: &fakeCreds{token: "tok"}, API: api,
	})

	_, err := svc.ListProjects(context.Background())
	if err == nil {
		t.Fatal("a 502 from Vercel must not be reported as a successful listing")
	}
	if errors.Is(err, vercelops.ErrListingUnavailable) || errors.Is(err, vercelops.ErrNotConnected) {
		t.Fatalf("err = %v, want a plain error so the HTTP layer keeps its 5xx", err)
	}
}

func TestListProjectsWalksEveryScopeAndStampsTheTeam(t *testing.T) {
	api := &fakeAPI{
		teams: []domain.VercelTeam{{ID: "team_1", Slug: "acme"}},
		projects: map[string][]domain.VercelProject{
			"":       {{ID: "prj_personal", Name: "site"}},
			"team_1": {{ID: "prj_team", Name: "admin"}},
		},
	}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(), Creds: &fakeCreds{token: "tok"}, API: api,
	})

	projects, err := svc.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want the personal one and the team one", len(projects))
	}
	byID := map[string]domain.VercelProject{}
	for _, p := range projects {
		byID[p.ID] = p
	}
	if got := byID["prj_team"].TeamID; got != "team_1" {
		t.Fatalf("team project TeamID = %q, want team_1", got)
	}
	if got := byID["prj_team"].TeamSlug; got != "acme" {
		t.Fatalf("team project TeamSlug = %q, want acme", got)
	}
	if got := byID["prj_personal"].TeamID; got != "" {
		t.Fatalf("personal project TeamID = %q, want the empty personal scope", got)
	}
}

func TestListProjectsSurvivesOneUnreachableTeam(t *testing.T) {
	api := &fakeAPI{
		teams:    []domain.VercelTeam{{ID: "team_ok", Slug: "ok"}, {ID: "team_bad", Slug: "bad"}},
		projects: map[string][]domain.VercelProject{"team_ok": {{ID: "prj_1", Name: "a"}}},
		scopeErr: map[string]error{"team_bad": unauthorized()},
	}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(), Creds: &fakeCreds{token: "tok"}, API: api,
	})

	projects, err := svc.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("one unreachable team must not fail the whole listing: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "prj_1" {
		t.Fatalf("got %v, want the one readable team's project", projects)
	}
}

func TestListProjectsReportsAnEmptyAccountAsSuccess(t *testing.T) {
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(), Creds: &fakeCreds{token: "tok"}, API: &fakeAPI{},
	})

	projects, err := svc.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("an account with no projects is not an error: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("got %v, want an empty listing", projects)
	}
}

func monorepo(paths ...string) domain.Repository {
	repo := domain.Repository{ID: uuid.New(), Name: "monorepo", Kind: domain.RepoKindMonorepo}
	for _, p := range paths {
		repo.SubProjects = append(repo.SubProjects, domain.RepoSubProject{Path: p, Kind: domain.RepoKindFrontend})
	}
	return repo
}

func TestLinkProjectRefusesAProjectTheTokenCannotRead(t *testing.T) {
	links := newFakeLinks()
	repo := monorepo("web")
	api := &fakeAPI{
		teams:   []domain.VercelTeam{{ID: "team_1", Slug: "acme"}},
		byScope: map[string]map[string]domain.VercelProject{},
	}
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: &fakeCreds{token: "tok"}, API: api, Repos: &fakeRepos{repo: repo},
	})

	_, err := svc.LinkProject(context.Background(), repo.ID, "web", "prj_gone")
	if !errors.Is(err, vercelops.ErrProjectUnreachable) {
		t.Fatalf("err = %v, want ErrProjectUnreachable", err)
	}
	if len(links.rows) != 0 {
		t.Fatalf("a refused link must persist nothing, got %v", links.rows)
	}
}

func TestLinkProjectStoresWhatVercelSaysNotWhatTheRequestClaimed(t *testing.T) {
	links := newFakeLinks()
	repo := monorepo("web")
	api := &fakeAPI{
		teams: []domain.VercelTeam{{ID: "team_1", Slug: "acme"}},
		byScope: map[string]map[string]domain.VercelProject{
			"team_1": {"prj_1": {
				ID: "prj_1", Name: "acme-web", Framework: "nextjs",
				RootDirectory: "web", ProductionURL: "https://acme.example",
			}},
		},
	}
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: &fakeCreds{token: "tok", team: "team_1"},
		API: api, Repos: &fakeRepos{repo: repo},
	})

	link, err := svc.LinkProject(context.Background(), repo.ID, "web", "prj_1")
	if err != nil {
		t.Fatal(err)
	}
	if link.ProjectName != "acme-web" || link.Framework != "nextjs" ||
		link.ProductionURL != "https://acme.example" || link.RootDirectory != "web" {
		t.Fatalf("link did not carry Vercel's own answer: %+v", link)
	}
	if link.TeamID != "team_1" || link.TeamSlug != "acme" {
		t.Fatalf("link scope = %q/%q, want team_1/acme", link.TeamID, link.TeamSlug)
	}
	if link.SubProjectPath != "web" {
		t.Fatalf("SubProjectPath = %q, want web", link.SubProjectPath)
	}
}

func TestLinkProjectRejectsASubProjectPathTheRepositoryDoesNotHave(t *testing.T) {
	repo := monorepo("web")
	api := &fakeAPI{byScope: map[string]map[string]domain.VercelProject{
		"": {"prj_1": {ID: "prj_1", Name: "x"}},
	}}
	links := newFakeLinks()
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: &fakeCreds{token: "tok"}, API: api, Repos: &fakeRepos{repo: repo},
	})

	_, err := svc.LinkProject(context.Background(), repo.ID, "does-not-exist", "prj_1")
	if !errors.Is(err, vercelops.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if len(links.rows) != 0 {
		t.Fatalf("nothing must be persisted, got %v", links.rows)
	}
}

func TestLinkProjectRejectsABlankProjectID(t *testing.T) {
	repo := monorepo("web")
	api := &fakeAPI{}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(), Creds: &fakeCreds{token: "tok"}, API: api, Repos: &fakeRepos{repo: repo},
	})

	if _, err := svc.LinkProject(context.Background(), repo.ID, "web", "  "); !errors.Is(err, vercelops.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if api.projectHi != 0 {
		t.Fatalf("a blank id must not reach Vercel, got %d calls", api.projectHi)
	}
}

func TestLinkProjectBindsTheWholeRepositoryOnAnEmptyPath(t *testing.T) {
	links := newFakeLinks()
	repo := domain.Repository{ID: uuid.New(), Name: "site", Kind: domain.RepoKindFrontend}
	api := &fakeAPI{byScope: map[string]map[string]domain.VercelProject{
		"": {"prj_1": {ID: "prj_1", Name: "site"}},
	}}
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: &fakeCreds{token: "tok"}, API: api, Repos: &fakeRepos{repo: repo},
	})

	link, err := svc.LinkProject(context.Background(), repo.ID, "", "prj_1")
	if err != nil {
		t.Fatal(err)
	}
	if link.SubProjectPath != "" {
		t.Fatalf("SubProjectPath = %q, want the whole-repository key", link.SubProjectPath)
	}
}

func TestLinkProjectReportsNotConnectedBeforeTouchingVercel(t *testing.T) {
	repo := monorepo("web")
	api := &fakeAPI{}
	svc := vercelops.NewService(vercelops.Deps{
		Links: newFakeLinks(), Creds: &fakeCreds{}, API: api, Repos: &fakeRepos{repo: repo},
	})

	_, err := svc.LinkProject(context.Background(), repo.ID, "web", "prj_1")
	if !errors.Is(err, vercelops.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
	if api.projectHi != 0 {
		t.Fatalf("no token means no Vercel call, got %d", api.projectHi)
	}
}

func linkedService(t *testing.T, api *fakeAPI, deps *fakeDeployments, creds *fakeCreds) (*vercelops.Service, uuid.UUID) {
	t.Helper()
	links := newFakeLinks()
	repoID := uuid.New()
	links.rows["web"] = domain.VercelProjectLink{
		RepositoryID: repoID, SubProjectPath: "web", ProjectID: "prj_1", ProjectName: "acme-web",
		TeamID: "team_1", TeamSlug: "acme", Framework: "nextjs", ProductionURL: "https://recorded.example",
	}
	svc := vercelops.NewService(vercelops.Deps{
		Links: links, Creds: creds, API: api, Deployments: deps,
		Repos: &fakeRepos{repo: domain.Repository{ID: repoID}},
	})
	return svc, repoID
}

func TestProjectDetailsReturnsTheLiveDeployment(t *testing.T) {
	shipped := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	api := &fakeAPI{byScope: map[string]map[string]domain.VercelProject{
		"team_1": {"prj_1": {ID: "prj_1", Name: "acme-web", Framework: "nextjs",
			ProductionURL: "https://live.example", RootDirectory: "web"}},
	}}
	deps := &fakeDeployments{byTarget: map[string][]domain.VercelDeployment{
		domain.VercelTargetProduction: {{
			ID: "dpl_1", State: domain.VercelDeploymentReady, Target: domain.VercelTargetProduction,
			CreatedAt: shipped, CommitSHA: "abc123", CommitMessage: "ship it",
		}},
	}}
	svc, repoID := linkedService(t, api, deps, &fakeCreds{token: "tok"})

	details, err := svc.ProjectDetails(context.Background(), repoID, "web")
	if err != nil {
		t.Fatal(err)
	}
	if details.ProductionURL != "https://live.example" {
		t.Fatalf("ProductionURL = %q, want the live one to win over the recorded one", details.ProductionURL)
	}
	if details.LatestDeployment == nil {
		t.Fatal("expected the latest deployment")
	}
	if details.LatestDeployment.CommitSHA != "abc123" || !details.LatestDeployment.CreatedAt.Equal(shipped) {
		t.Fatalf("latest deployment lost its commit or its time: %+v", details.LatestDeployment)
	}
	if details.LastFailedDeployment != nil {
		t.Fatalf("a green project has no failed build, got %+v", details.LastFailedDeployment)
	}
	if len(details.Warnings) != 0 {
		t.Fatalf("a complete read carries no warnings, got %v", details.Warnings)
	}
}

func TestProjectDetailsSurvivesAVercelOutage(t *testing.T) {
	boom := &vercelapi.APIError{Status: http.StatusBadGateway, Message: "upstream"}
	api := &fakeAPI{scopeErr: map[string]error{"team_1": boom}}
	deps := &fakeDeployments{err: boom}
	svc, repoID := linkedService(t, api, deps, &fakeCreds{token: "tok"})

	details, err := svc.ProjectDetails(context.Background(), repoID, "web")
	if err != nil {
		t.Fatalf("a Vercel outage must not fail the details read: %v", err)
	}
	if details.Link.ProjectID != "prj_1" || details.ProductionURL != "https://recorded.example" {
		t.Fatalf("the recorded values must survive: %+v", details)
	}
	if details.LatestDeployment != nil || details.LastFailedDeployment != nil {
		t.Fatal("nothing could be read, so no deployment may be reported")
	}
	if len(details.Warnings) < 2 {
		t.Fatalf("both failed reads must be reported as warnings, got %v", details.Warnings)
	}
}

func TestProjectDetailsWithoutAConnectionStillRendersTheLink(t *testing.T) {
	svc, repoID := linkedService(t, &fakeAPI{}, &fakeDeployments{}, &fakeCreds{})

	details, err := svc.ProjectDetails(context.Background(), repoID, "web")
	if err != nil {
		t.Fatalf("no connection must not fail the details read: %v", err)
	}
	if details.Link.ProjectName != "acme-web" || details.Framework != "nextjs" {
		t.Fatalf("the recorded values must survive: %+v", details)
	}
	if len(details.Warnings) == 0 {
		t.Fatal("the missing connection must be reported as a warning")
	}
}

func TestProjectDetailsReportsTheLastFailedBuildBesideAGreenLatest(t *testing.T) {
	api := &fakeAPI{byScope: map[string]map[string]domain.VercelProject{
		"team_1": {"prj_1": {ID: "prj_1", Name: "acme-web"}},
	}}
	deps := &fakeDeployments{byTarget: map[string][]domain.VercelDeployment{
		domain.VercelTargetProduction: {
			{ID: "dpl_new", State: domain.VercelDeploymentReady},
			{ID: "dpl_old", State: domain.VercelDeploymentError,
				ErrorCode: "BUILD_FAILED", ErrorMessage: "Command \"npm run build\" exited with 1"},
		},
	}}
	svc, repoID := linkedService(t, api, deps, &fakeCreds{token: "tok"})

	details, err := svc.ProjectDetails(context.Background(), repoID, "web")
	if err != nil {
		t.Fatal(err)
	}
	if details.LatestDeployment == nil || details.LatestDeployment.ID != "dpl_new" {
		t.Fatalf("latest = %+v, want dpl_new", details.LatestDeployment)
	}
	if details.LastFailedDeployment == nil || details.LastFailedDeployment.ID != "dpl_old" {
		t.Fatalf("last failed = %+v, want dpl_old", details.LastFailedDeployment)
	}
	if details.LastFailedDeployment.ErrorCode != "BUILD_FAILED" {
		t.Fatalf("the build error must survive: %+v", details.LastFailedDeployment)
	}
}

func TestProjectDetailsFallsBackToPreviewDeployments(t *testing.T) {
	api := &fakeAPI{byScope: map[string]map[string]domain.VercelProject{
		"team_1": {"prj_1": {ID: "prj_1", Name: "acme-web"}},
	}}
	deps := &fakeDeployments{byTarget: map[string][]domain.VercelDeployment{
		domain.VercelTargetProduction: {},
		"":                            {{ID: "dpl_preview", State: domain.VercelDeploymentReady}},
	}}
	svc, repoID := linkedService(t, api, deps, &fakeCreds{token: "tok"})

	details, err := svc.ProjectDetails(context.Background(), repoID, "web")
	if err != nil {
		t.Fatal(err)
	}
	if details.LatestDeployment == nil || details.LatestDeployment.ID != "dpl_preview" {
		t.Fatalf("latest = %+v, want the preview deployment", details.LatestDeployment)
	}
	if details.LatestDeployment.Target == domain.VercelTargetProduction {
		t.Fatal("a preview build must not be reported as production")
	}
}

func TestProjectDetailsReportsAMissingLinkAsNotFound(t *testing.T) {
	svc, repoID := linkedService(t, &fakeAPI{}, &fakeDeployments{}, &fakeCreds{token: "tok"})

	if _, err := svc.ProjectDetails(context.Background(), repoID, "api"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want port.ErrNotFound", err)
	}
}
