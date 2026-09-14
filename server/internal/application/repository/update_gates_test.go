package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func newUpdateTestService(repo domain.Repository) (*Service, *fakeReleaseRepoStore) {
	repos := &fakeReleaseRepoStore{repo: repo}
	return &Service{repos: repos}, repos
}

func TestUpdateSetsTheMobilePlatformOnAMobileRepo(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMobile})
	platform := domain.MobilePlatformAndroid

	repo, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{MobilePlatform: &platform})
	require.NoError(t, err)
	require.Equal(t, domain.MobilePlatformAndroid, repo.MobilePlatform)
	require.Equal(t, []string{domain.MobilePlatformAndroid}, repos.mobilePlatformWrites)
}

// The kind in the same PATCH is what the platform is judged against, so
// "make this mobile and it is an iOS app" is one request, not two.
func TestUpdateAcceptsAPlatformAlongsideTheKindThatJustifiesIt(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	kind := domain.RepoKindMobile
	platform := domain.MobilePlatformIOS

	repo, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{
		Kind: &kind, MobilePlatform: &platform,
	})
	require.NoError(t, err)
	require.Equal(t, domain.MobilePlatformIOS, repo.MobilePlatform)
}

func TestUpdateRejectsAPlatformOnANonMobileRepo(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	platform := domain.MobilePlatformIOS

	_, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{MobilePlatform: &platform})
	require.ErrorContains(t, err, "only meaningful on a mobile project")
	require.Empty(t, repos.mobilePlatformWrites)
}

func TestUpdateRejectsAnUnknownPlatform(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMobile})
	platform := "symbian"

	_, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{MobilePlatform: &platform})
	require.ErrorContains(t, err, "invalid mobile platform")
	require.Empty(t, repos.mobilePlatformWrites)
}

// Clearing is always allowed: "" is the unset value, whatever the kind.
func TestUpdateClearsThePlatformOnAnyKind(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{
		ID: uuid.New(), Kind: domain.RepoKindBackend, MobilePlatform: domain.MobilePlatformIOS,
	})
	empty := ""

	repo, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{MobilePlatform: &empty})
	require.NoError(t, err)
	require.Empty(t, repo.MobilePlatform)
	require.Equal(t, []string{""}, repos.mobilePlatformWrites)
}

func TestUpdateSetsTheMutationGate(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	on := true
	threshold := 65.0

	repo, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{
		MutationEnabled: &on, MutationThreshold: &threshold,
	})
	require.NoError(t, err)
	require.True(t, repo.MutationEnabled)
	require.Equal(t, 65.0, repo.MutationThreshold)
}

func TestUpdateRejectsAMutationThresholdOutside0To100(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	bad := 140.0

	_, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{MutationThreshold: &bad})
	require.ErrorContains(t, err, "must be between 0 and 100")
	require.Zero(t, repos.repo.MutationThreshold)
}

func TestUpdateValidatesSubProjectQualityGates(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMonorepo})
	bad := -5.0
	subs := []domain.RepoSubProject{{Path: "apps/api", Kind: domain.RepoKindBackend, MutationThreshold: &bad}}

	_, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{SubProjects: &subs})
	require.ErrorContains(t, err, "mutation_threshold must be between 0 and 100")
	require.Empty(t, repos.subProjectWrites)
}

func TestUpdateSetsTheReleaseEngineOnAMobileRepo(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMobile})
	engine := domain.ReleaseEngineLocal

	repo, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &engine})
	require.NoError(t, err)
	require.Equal(t, domain.ReleaseEngineLocal, repo.ReleaseEngine)
	require.Equal(t, []string{domain.ReleaseEngineLocal}, repos.releaseEngineWrites)
}

func TestUpdateRejectsAPinnedEngineOnANonMobileRepo(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	engine := domain.ReleaseEngineActions

	_, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &engine})
	require.ErrorContains(t, err, "only meaningful on a mobile project")
	require.Empty(t, repos.releaseEngineWrites)
}

func TestUpdateRejectsAnUnknownReleaseEngine(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMobile})
	engine := "jenkins"

	_, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &engine})
	require.ErrorContains(t, err, "invalid release engine")
	require.Empty(t, repos.releaseEngineWrites)
}

// "" is a legal statement here, unlike mobile_platform's unset value: it means
// the column's default. The column itself must never hold a blank, because
// ValidReleaseEngine("") is false and every reader downstream treats it as a bug.
func TestUpdateWritesAutoForAnEmptyReleaseEngine(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{
		ID: uuid.New(), Kind: domain.RepoKindMobile, ReleaseEngine: domain.ReleaseEngineLocal,
	})
	empty := ""

	repo, err := svc.Update(repoCtx(repoTenantA), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &empty})
	require.NoError(t, err)
	require.Equal(t, domain.ReleaseEngineAuto, repo.ReleaseEngine)
	require.Equal(t, []string{domain.ReleaseEngineAuto}, repos.releaseEngineWrites)
}
