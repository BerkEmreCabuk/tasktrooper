package hosting_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/hosting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repofacts"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// --- fakes -----------------------------------------------------------------

type fakeLinks struct {
	rows map[string]domain.HostingLink // area → link
}

func newFakeLinks() *fakeLinks { return &fakeLinks{rows: map[string]domain.HostingLink{}} }

func (f *fakeLinks) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.HostingLink, error) {
	var out []domain.HostingLink
	for _, l := range f.rows {
		out = append(out, l)
	}
	return out, nil
}
func (f *fakeLinks) Get(ctx context.Context, repositoryID uuid.UUID, area string) (domain.HostingLink, error) {
	l, ok := f.rows[area]
	if !ok {
		return domain.HostingLink{}, port.ErrNotFound
	}
	return l, nil
}
func (f *fakeLinks) Save(ctx context.Context, link domain.HostingLink) (domain.HostingLink, error) {
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	f.rows[link.Area] = link
	return link, nil
}
func (f *fakeLinks) Delete(ctx context.Context, repositoryID uuid.UUID, area string) error {
	delete(f.rows, area)
	return nil
}

type fakeRepos struct{ repo domain.Repository }

func (f *fakeRepos) Get(ctx context.Context, id uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

type fakeCreds struct {
	token string
	team  string
}

func (f *fakeCreds) VercelToken(ctx context.Context) (string, error) { return f.token, nil }
func (f *fakeCreds) SetVercelToken(ctx context.Context, token string) error {
	f.token = token
	return nil
}
func (f *fakeCreds) DeleteVercelToken(ctx context.Context) error {
	f.token, f.team = "", ""
	return nil
}
func (f *fakeCreds) VercelTeam(ctx context.Context) (string, error) { return f.team, nil }
func (f *fakeCreds) SetVercelTeam(ctx context.Context, teamID string) error {
	f.team = teamID
	return nil
}

type fakeVercel struct {
	user     domain.VercelUser
	userErr  error
	teams    []domain.VercelTeam
	projects map[string][]domain.VercelProject // teamID → projects
	// projectCalls records Project(ctx, …) lookups.
	projectCalls []string
}

func (f *fakeVercel) User(ctx context.Context, token string) (domain.VercelUser, error) {
	if f.userErr != nil {
		return domain.VercelUser{}, f.userErr
	}
	return f.user, nil
}
func (f *fakeVercel) Teams(ctx context.Context, token string) ([]domain.VercelTeam, error) {
	return f.teams, nil
}
func (f *fakeVercel) Projects(ctx context.Context, token, teamID string) ([]domain.VercelProject, error) {
	return f.projects[teamID], nil
}
func (f *fakeVercel) Project(ctx context.Context, token, teamID, idOrName string) (domain.VercelProject, error) {
	f.projectCalls = append(f.projectCalls, teamID+"/"+idOrName)
	for _, ps := range f.projects {
		for _, p := range ps {
			if p.ID == idOrName || p.Name == idOrName {
				return p, nil
			}
		}
	}
	return domain.VercelProject{}, errors.New("not found")
}

type fakeTargets struct {
	rows map[string]domain.DeployTarget
}

func newFakeTargets() *fakeTargets { return &fakeTargets{rows: map[string]domain.DeployTarget{}} }

func (f *fakeTargets) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeTargets) ListAll(ctx context.Context) ([]domain.DeployTarget, error) { return nil, nil }
func (f *fakeTargets) Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	t, ok := f.rows[env]
	if !ok {
		return domain.DeployTarget{}, port.ErrNotFound
	}
	return t, nil
}
func (f *fakeTargets) Save(ctx context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	f.rows[t.Env] = t
	return t, nil
}
func (f *fakeTargets) Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	delete(f.rows, env)
	return nil
}

func factsWith(fn func(f *repofacts.Facts)) func(ctx context.Context, root string) repofacts.Facts {
	return func(ctx context.Context, root string) repofacts.Facts {
		f := repofacts.Facts{Root: root, FileCount: 10}
		fn(&f)
		return f
	}
}

var (
	webProject = domain.VercelProject{
		ID: "prj_web", Name: "tt-web", Framework: "vite", RootDirectory: "apps/web",
		Link:          &domain.VercelGitLink{Type: "github", Org: "acme-org", Repo: "mono"},
		ProductionURL: "https://app.example.com",
	}
	docsProject = domain.VercelProject{
		ID: "prj_docs", Name: "tt-docs", RootDirectory: "apps/docs",
		Link:          &domain.VercelGitLink{Type: "github", Org: "acme-org", Repo: "mono"},
		ProductionURL: "https://docs.example.com",
	}
	soloProject = domain.VercelProject{
		ID: "prj_solo", Name: "solo",
		Link:          &domain.VercelGitLink{Type: "github", Org: "acme-org", Repo: "solo"},
		ProductionURL: "https://solo.example.com",
	}
)

// --- detection ----------------------------------------------------------------

func TestDetectSingleFrontendRepoGitLinkIsExact(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "solo", Kind: domain.RepoKindFrontend, RootPath: t.TempDir()}
	api := &fakeVercel{projects: map[string][]domain.VercelProject{"team_1": {soloProject, webProject}}}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{token: "tok", team: "team_1"}, api)
	svc.SetFactsCollector(factsWith(func(f *repofacts.Facts) {
		f.Git.RemoteSlug = "acme-org/solo"
		f.Integrations = []repofacts.Integration{{Name: "Vercel", Category: "hosting", Evidence: "vercel.json"}}
	}))

	det, err := svc.Detect(context.Background(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !det.VercelConnected || len(det.Areas) != 1 {
		t.Fatalf("det = %+v", det)
	}
	a := det.Areas[0]
	if a.Area != domain.HostingAreaRoot || a.Kind != domain.RepoKindFrontend {
		t.Fatalf("area = %+v", a)
	}
	if a.Confidence != domain.HostingConfidenceExact || len(a.Candidates) != 1 || a.Candidates[0].Project.ID != "prj_solo" {
		t.Fatalf("expected the single git-linked project to be exact, got %+v", a)
	}
	if a.Candidates[0].Reason != domain.HostingMatchGitLink {
		t.Fatalf("reason = %s", a.Candidates[0].Reason)
	}
	if len(a.Hints) != 1 || a.Hints[0].Provider != domain.DeployProviderVercel {
		t.Fatalf("hints = %+v", a.Hints)
	}
}

func TestDetectMonorepoSplitsAreasByRootDirectory(t *testing.T) {
	repo := domain.Repository{
		ID: uuid.New(), Name: "mono", Kind: domain.RepoKindMonorepo,
		SubRepoKinds: []string{domain.RepoKindBackend, domain.RepoKindFrontend}, RootPath: t.TempDir(),
	}
	api := &fakeVercel{projects: map[string][]domain.VercelProject{"": {webProject, docsProject}}}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{token: "tok"}, api)
	svc.SetFactsCollector(factsWith(func(f *repofacts.Facts) {
		f.Git.RemoteSlug = "acme-org/mono"
		f.KindEvidence = []string{"apps/api → backend", "apps/web → frontend"}
		f.Integrations = []repofacts.Integration{
			{Name: "Vercel", Category: "hosting", Evidence: "apps/web/vercel.json"},
			{Name: "Fly.io", Category: "hosting", Evidence: "apps/api/fly.toml"},
		}
		f.Deploys = []repofacts.DeployTarget{{Provider: "GitHub Actions", Environment: "production", Trigger: "push:main", Evidence: ".github/workflows/deploy-api.yml"}}
	}))

	det, err := svc.Detect(context.Background(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Areas) != 2 {
		t.Fatalf("areas = %+v", det.Areas)
	}
	fe, be := det.Areas[0], det.Areas[1]
	if fe.Area != domain.RepoKindFrontend || fe.Directory != "apps/web" {
		t.Fatalf("frontend area = %+v", fe)
	}
	if fe.Confidence != domain.HostingConfidenceExact || fe.Candidates[0].Project.ID != "prj_web" || fe.Candidates[0].Reason != domain.HostingMatchGitLinkDir {
		t.Fatalf("frontend should resolve to the apps/web project exactly, got %+v", fe.Candidates)
	}
	// The docs project is git-linked to the same repo but built from another
	// folder: it must still be offered, just not as the decisive answer.
	if len(fe.Candidates) != 2 || fe.Candidates[1].Reason != domain.HostingMatchGitLink {
		t.Fatalf("frontend candidates = %+v", fe.Candidates)
	}
	if len(fe.Hints) != 2 || fe.Hints[0].Evidence != "apps/web/vercel.json" || fe.Hints[1].Provider != "github_actions" {
		t.Fatalf("frontend hints = %+v", fe.Hints)
	}

	if be.Area != domain.RepoKindBackend || be.Directory != "apps/api" {
		t.Fatalf("backend area = %+v", be)
	}
	// Both projects are git-linked to the repo but neither is built from
	// apps/api, so the backend is ambiguous — the UI must ask.
	if be.Confidence != domain.HostingConfidenceAmbiguous {
		t.Fatalf("backend confidence = %s (%+v)", be.Confidence, be.Candidates)
	}
	if len(be.Hints) != 2 || be.Hints[0].Provider != domain.DeployProviderFly {
		t.Fatalf("backend hints = %+v", be.Hints)
	}
}

func TestDetectProjectJSONWinsAndReachesOtherTeam(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".vercel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vercel", "project.json"), []byte(`{"projectId":"prj_other","orgId":"team_2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := domain.Repository{ID: uuid.New(), Name: "solo", Kind: domain.RepoKindBackend, RootPath: root}
	other := domain.VercelProject{ID: "prj_other", Name: "other", ProductionURL: "https://other.example.com"}
	api := &fakeVercel{projects: map[string][]domain.VercelProject{"team_1": {soloProject}, "team_2": {other}}}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{token: "tok", team: "team_1"}, api)
	svc.SetFactsCollector(factsWith(func(f *repofacts.Facts) { f.Git.RemoteSlug = "acme-org/solo" }))

	det, err := svc.Detect(context.Background(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := det.Areas[0]
	if a.Confidence != domain.HostingConfidenceExact || a.Candidates[0].Project.ID != "prj_other" || a.Candidates[0].Reason != domain.HostingMatchProjectJSON {
		t.Fatalf("link file must win: %+v", a.Candidates)
	}
	if len(api.projectCalls) != 1 || api.projectCalls[0] != "team_2/prj_other" {
		t.Fatalf("expected a direct lookup in the file's team, got %v", api.projectCalls)
	}
	// The git-linked project in the default team is still listed, second.
	if len(a.Candidates) != 2 || a.Candidates[1].Project.ID != "prj_solo" {
		t.Fatalf("candidates = %+v", a.Candidates)
	}
}

func TestDetectWithoutConnectionReportsHintsOnly(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "solo", Kind: domain.RepoKindFrontend, RootPath: t.TempDir()}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{}, &fakeVercel{})
	svc.SetFactsCollector(factsWith(func(f *repofacts.Facts) {
		f.Integrations = []repofacts.Integration{{Name: "Netlify", Category: "hosting", Evidence: "netlify.toml"}}
	}))
	det, err := svc.Detect(context.Background(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if det.VercelConnected {
		t.Fatal("no token, must not claim a connection")
	}
	a := det.Areas[0]
	if a.Confidence != domain.HostingConfidenceNone || len(a.Candidates) != 0 || len(a.Hints) != 1 || a.Hints[0].Provider != "netlify" {
		t.Fatalf("area = %+v", a)
	}
}

func TestDetectSkipsMobileRepos(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "app", Kind: domain.RepoKindMobile, RootPath: t.TempDir()}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{token: "tok"}, &fakeVercel{})
	svc.SetFactsCollector(factsWith(func(f *repofacts.Facts) {}))
	det, err := svc.Detect(context.Background(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Areas) != 0 || len(det.Warnings) == 0 {
		t.Fatalf("det = %+v", det)
	}
}

// --- linking ------------------------------------------------------------------

func TestLinkVercelResolvesProjectAndFillsProdTarget(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "solo", Kind: domain.RepoKindFrontend}
	links := newFakeLinks()
	targets := newFakeTargets()
	api := &fakeVercel{
		user:     domain.VercelUser{Username: "akif"},
		teams:    []domain.VercelTeam{{ID: "team_1", Slug: "tasktrooper", Name: "TaskTrooper"}},
		projects: map[string][]domain.VercelProject{"team_1": {soloProject}},
	}
	svc := hosting.NewService(links, &fakeRepos{repo: repo}, &fakeCreds{token: "tok", team: "team_1"}, api)
	svc.SetDeployTargets(targets)

	link, err := svc.Link(context.Background(), repo.ID, domain.SaveHostingLinkRequest{
		Area: "root", Provider: "vercel", ExternalID: "prj_solo", Source: "detected", Evidence: "git link",
	})
	if err != nil {
		t.Fatal(err)
	}
	if link.ExternalName != "solo" || link.ScopeID != "team_1" || link.ScopeSlug != "tasktrooper" || link.ProductionURL != "https://solo.example.com" {
		t.Fatalf("link = %+v", link)
	}
	if link.Source != domain.HostingSourceDetected || link.Area != domain.HostingAreaRoot {
		t.Fatalf("link = %+v", link)
	}

	prod, ok := targets.rows[domain.DeployEnvProd]
	if !ok {
		t.Fatal("prod target was not created")
	}
	if prod.Provider != domain.DeployProviderVercel || prod.TemplateID != "vercel" {
		t.Fatalf("prod = %+v", prod)
	}
	if prod.BaseURL != "https://solo.example.com" || prod.HealthURL != "https://solo.example.com" {
		t.Fatalf("prod urls = %s / %s", prod.BaseURL, prod.HealthURL)
	}
	if prod.Vars["vercel_scope"] != "tasktrooper" || prod.Vars["vercel_project"] != "solo" || prod.Vars["vercel_project_id"] != "prj_solo" || prod.Vars["vercel_team_id"] != "team_1" {
		t.Fatalf("prod vars = %+v", prod.Vars)
	}
}

func TestLinkVercelLeavesForeignProdTargetAlone(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "solo", Kind: domain.RepoKindBackend}
	targets := newFakeTargets()
	targets.rows[domain.DeployEnvProd] = domain.DeployTarget{Env: domain.DeployEnvProd, Provider: domain.DeployProviderGCPCloudRun, BaseURL: "https://api.example.com"}
	api := &fakeVercel{user: domain.VercelUser{Username: "akif"}, projects: map[string][]domain.VercelProject{"": {soloProject}}}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{token: "tok"}, api)
	svc.SetDeployTargets(targets)

	link, err := svc.Link(context.Background(), repo.ID, domain.SaveHostingLinkRequest{Area: "", Provider: "vercel", ExternalID: "prj_solo"})
	if err != nil {
		t.Fatal(err)
	}
	// Personal account: the scope slug is the username.
	if link.ScopeSlug != "akif" || link.ScopeID != "" {
		t.Fatalf("link = %+v", link)
	}
	prod := targets.rows[domain.DeployEnvProd]
	if prod.Provider != domain.DeployProviderGCPCloudRun || prod.BaseURL != "https://api.example.com" || len(prod.Vars) != 0 {
		t.Fatalf("a human's Cloud Run target was rewritten: %+v", prod)
	}
}

func TestLinkMonorepoAreaDoesNotTouchProdTarget(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "mono", Kind: domain.RepoKindMonorepo, SubRepoKinds: []string{"frontend", "backend"}}
	targets := newFakeTargets()
	api := &fakeVercel{user: domain.VercelUser{Username: "akif"}, projects: map[string][]domain.VercelProject{"": {webProject}}}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{token: "tok"}, api)
	svc.SetDeployTargets(targets)

	link, err := svc.Link(context.Background(), repo.ID, domain.SaveHostingLinkRequest{Area: "frontend", Provider: "vercel", ExternalID: "prj_web"})
	if err != nil {
		t.Fatal(err)
	}
	if link.Area != "frontend" || link.RootDirectory != "apps/web" {
		t.Fatalf("link = %+v", link)
	}
	if len(targets.rows) != 0 {
		t.Fatalf("a monorepo area must not define the whole repo's prod target: %+v", targets.rows)
	}
}

func TestLinkOtherProviderRecordsAnswerWithoutAPI(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "api", Kind: domain.RepoKindBackend}
	links := newFakeLinks()
	svc := hosting.NewService(links, &fakeRepos{repo: repo}, &fakeCreds{}, &fakeVercel{userErr: errors.New("must not be called")})

	link, err := svc.Link(context.Background(), repo.ID, domain.SaveHostingLinkRequest{Area: "root", Provider: "gcp_cloud_run", Evidence: "operator said so"})
	if err != nil {
		t.Fatal(err)
	}
	if link.Provider != domain.DeployProviderGCPCloudRun || link.Source != domain.HostingSourceUser || link.ExternalID != "" {
		t.Fatalf("link = %+v", link)
	}
	if _, ok := links.rows[domain.HostingAreaRoot]; !ok {
		t.Fatal("link not stored")
	}
	if err := svc.Unlink(context.Background(), repo.ID, "root"); err != nil {
		t.Fatal(err)
	}
	if len(links.rows) != 0 {
		t.Fatal("unlink did not delete")
	}
}

func TestLinkVercelWithoutConnectionIsRefused(t *testing.T) {
	repo := domain.Repository{ID: uuid.New(), Name: "solo", Kind: domain.RepoKindFrontend}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{repo: repo}, &fakeCreds{}, &fakeVercel{})
	_, err := svc.Link(context.Background(), repo.ID, domain.SaveHostingLinkRequest{Area: "root", Provider: "vercel", ExternalID: "prj_solo"})
	if !errors.Is(err, hosting.ErrNotConnected) {
		t.Fatalf("err = %v", err)
	}
	_, err = svc.Link(context.Background(), repo.ID, domain.SaveHostingLinkRequest{Area: "kitchen", Provider: "vercel"})
	if !errors.Is(err, hosting.ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
}

// --- connection ---------------------------------------------------------------

func TestConnectVerifiesTokenAndTeam(t *testing.T) {
	creds := &fakeCreds{}
	api := &fakeVercel{
		user:  domain.VercelUser{Username: "akif", Email: "a@b.c"},
		teams: []domain.VercelTeam{{ID: "team_1", Slug: "tasktrooper"}},
	}
	svc := hosting.NewService(newFakeLinks(), &fakeRepos{}, creds, api)

	if _, err := svc.Connect(context.Background(), "  ", ""); !errors.Is(err, hosting.ErrInvalidInput) {
		t.Fatalf("blank token: %v", err)
	}
	if _, err := svc.Connect(context.Background(), "tok", "team_nope"); !errors.Is(err, hosting.ErrInvalidInput) {
		t.Fatalf("foreign team: %v", err)
	}
	if creds.token != "" {
		t.Fatal("token must not be stored before the team is validated")
	}

	st, err := svc.Connect(context.Background(), "tok", "team_1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.Username != "akif" || st.TeamID != "team_1" || st.TeamSlug != "tasktrooper" {
		t.Fatalf("status = %+v", st)
	}
	if creds.token != "tok" || creds.team != "team_1" {
		t.Fatalf("creds = %+v", creds)
	}

	st, err = svc.SetTeam(context.Background(), "")
	if err != nil || st.TeamID != "" || st.TeamSlug != "" {
		t.Fatalf("SetTeam personal: %+v %v", st, err)
	}

	api.userErr = errors.New("vercel api: 403 Not authorized")
	st, err = svc.Status(context.Background())
	if err != nil || st.Connected || !strings.Contains(st.Detail, "403") {
		t.Fatalf("revoked token must read as disconnected with detail: %+v %v", st, err)
	}

	if err := svc.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if creds.token != "" || creds.team != "" {
		t.Fatalf("disconnect left creds: %+v", creds)
	}
}
