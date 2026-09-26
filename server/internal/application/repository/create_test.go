package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type createGit struct {
	fakeReleaseGit

	mu         sync.Mutex
	ensureErrs []error
	ensured    []string
	origins    map[string]string
}

func (g *createGit) HasGit(root string) bool {
	_, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil
}

func (g *createGit) EnsureRepoWithRemote(_ context.Context, root, name, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensured = append(g.ensured, name)
	if len(g.ensureErrs) > 0 {
		err := g.ensureErrs[0]
		g.ensureErrs = g.ensureErrs[1:]
		if err != nil {
			return err
		}
	}
	return os.Mkdir(filepath.Join(root, ".git"), 0o755)
}

func (g *createGit) OriginURL(_ context.Context, root string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.origins[root]
}

type createRepoStore struct {
	*fakeReleaseRepoStore

	byRoot         map[string]domain.Repository
	deleted        []uuid.UUID
	setProjectsErr error
}

func newCreateRepoStore() *createRepoStore {
	return &createRepoStore{fakeReleaseRepoStore: &fakeReleaseRepoStore{}, byRoot: map[string]domain.Repository{}}
}

func (f *createRepoStore) Create(ctx context.Context, name, description, rootPath, remoteURL, kind string) (domain.Repository, error) {
	repo, err := f.fakeReleaseRepoStore.Create(ctx, name, description, rootPath, remoteURL, kind)
	if err != nil {
		return domain.Repository{}, err
	}
	f.byRoot[rootPath] = repo
	return repo, nil
}

func (f *createRepoStore) GetByRootPath(_ context.Context, rootPath string) (domain.Repository, error) {
	if repo, ok := f.byRoot[rootPath]; ok {
		return repo, nil
	}
	return domain.Repository{}, port.ErrNotFound
}

func (f *createRepoStore) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	for root, repo := range f.byRoot {
		if repo.ID == id {
			delete(f.byRoot, root)
		}
	}
	return nil
}

func (f *createRepoStore) SetProjects(context.Context, uuid.UUID, []uuid.UUID) error {
	return f.setProjectsErr
}

type fakeRefresher struct {
	mu           sync.Mutex
	async        []string
	neverScanned []string
}

func (f *fakeRefresher) RefreshAsync(_ context.Context, _ uuid.UUID, reason string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.async = append(f.async, reason)
	return true
}

func (f *fakeRefresher) RefreshIfStale(context.Context, uuid.UUID, string) {}

func (f *fakeRefresher) RefreshAfterPush(context.Context, uuid.UUID, string) {}

func (f *fakeRefresher) RefreshIfNeverScanned(_ context.Context, _ uuid.UUID, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.neverScanned = append(f.neverScanned, reason)
}

type createHarness struct {
	svc       *Service
	git       *createGit
	repos     *createRepoStore
	refresher *fakeRefresher
	reposDir  string
}

func newCreateHarness(t *testing.T) createHarness {
	t.Helper()
	root := t.TempDir()
	h := createHarness{
		git:       &createGit{origins: map[string]string{}},
		repos:     newCreateRepoStore(),
		refresher: &fakeRefresher{},
		reposDir:  filepath.Join(root, "repos"),
	}
	h.svc = &Service{
		repos:          h.repos,
		git:            h.git,
		workspaceRoot:  root,
		modelRefresher: h.refresher,
		gitWarnings:    map[uuid.UUID]string{},
	}
	return h
}

func TestCreateUsesTheSanitizedNameForTheFolderAndGitHub(t *testing.T) {
	h := newCreateHarness(t)

	repo, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "My App/../x"})
	require.NoError(t, err)

	require.Equal(t, "my-app..x", repo.Name)
	require.Equal(t, filepath.Join(h.reposDir, "my-app..x"), repo.RootPath)
	require.Equal(t, []string{"my-app..x"}, h.git.ensured)
	entries, err := os.ReadDir(h.reposDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestCreateRejectsANameWithNothingUsable(t *testing.T) {
	h := newCreateHarness(t)
	_, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "../.."})
	require.ErrorContains(t, err, "no letters or digits")
	require.Empty(t, h.git.ensured)
}

func TestCreateRefusesAnExistingDirectory(t *testing.T) {
	h := newCreateHarness(t)
	require.NoError(t, os.MkdirAll(filepath.Join(h.reposDir, "app"), 0o755))
	_, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "App"})
	require.ErrorContains(t, err, "directory already exists")
	require.Empty(t, h.git.ensured)
}

func TestCreateRemovesTheFolderWhenGitSetupFailsSoARetryWorks(t *testing.T) {
	h := newCreateHarness(t)
	h.git.ensureErrs = []error{errors.New("gh repo create: not logged in")}

	_, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "app"})
	require.ErrorContains(t, err, "git/GitHub setup failed: gh repo create: not logged in")
	require.NotContains(t, err.Error(), "was already created")
	_, statErr := os.Stat(filepath.Join(h.reposDir, "app"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	require.Empty(t, h.repos.byRoot)

	repo, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "app"})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(h.reposDir, "app"), repo.RootPath)
}

func TestCreateSaysWhenTheGitHubRepositoryWasAlreadyCreated(t *testing.T) {
	h := newCreateHarness(t)
	h.git.ensureErrs = []error{&domain.RemoteRepoCreatedError{URL: "https://github.com/acme/app", Err: errors.New("git push: denied")}}

	_, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "app"})
	require.ErrorContains(t, err, "git push: denied")
	require.ErrorContains(t, err, "https://github.com/acme/app was already created")
	var created *domain.RemoteRepoCreatedError
	require.ErrorAs(t, err, &created)
	_, statErr := os.Stat(filepath.Join(h.reposDir, "app"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestCreateFallsBackToTheOriginToTellTheRemoteExists(t *testing.T) {
	h := newCreateHarness(t)
	h.git.ensureErrs = []error{errors.New("gh repo create: push failed")}
	h.git.origins[filepath.Join(h.reposDir, "app")] = "https://github.com/acme/app.git"

	_, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "app"})
	require.ErrorContains(t, err, "https://github.com/acme/app.git was already created")
}

func TestCreateDropsAHalfRegisteredRowWhenLinkingProjectsFails(t *testing.T) {
	h := newCreateHarness(t)
	h.repos.setProjectsErr = errors.New("unknown project")

	_, err := h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "app", ProjectIDs: []uuid.UUID{uuid.New()}})
	require.ErrorContains(t, err, "unknown project")
	require.Len(t, h.repos.deleted, 1)
	require.Empty(t, h.repos.byRoot)
	_, statErr := os.Stat(filepath.Join(h.reposDir, "app"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestCreateScansButCreateWithoutScanDoesNot(t *testing.T) {
	h := newCreateHarness(t)

	_, err := h.svc.CreateWithoutScan(context.Background(), domain.CreateRepositoryRequest{Name: "fresh"})
	require.NoError(t, err)
	require.Empty(t, h.refresher.async)

	_, err = h.svc.Create(context.Background(), domain.CreateRepositoryRequest{Name: "legacy"})
	require.NoError(t, err)
	require.Equal(t, []string{"import"}, h.refresher.async)
	require.Empty(t, h.refresher.neverScanned)
}

func TestOpenOfARegisteredFolderStartsTheFirstScanIfThereIsNone(t *testing.T) {
	h := newCreateHarness(t)
	repo, err := h.svc.CreateWithoutScan(context.Background(), domain.CreateRepositoryRequest{Name: "app"})
	require.NoError(t, err)

	reopened, err := h.svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: repo.RootPath})
	require.NoError(t, err)
	require.Equal(t, repo.ID, reopened.ID)
	require.Equal(t, []string{"import"}, h.refresher.neverScanned)
	require.Empty(t, h.refresher.async)
}
