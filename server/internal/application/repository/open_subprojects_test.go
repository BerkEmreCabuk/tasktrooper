package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func monorepoLayout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"apps/api/go.mod":       "module example.com/api\n",
		"apps/web/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

func newOpenTestService(getByRootPathErr error) (*Service, *fakeReleaseRepoStore) {
	repos := &fakeReleaseRepoStore{getByRootPathErr: getByRootPathErr}
	svc := &Service{
		repos: repos,
		git:   &fakeReleaseGit{hasGit: true},
	}
	return svc, repos
}

func TestServiceOpen_DetectsAndPersistsSubProjectsForANewMonorepo(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := monorepoLayout(t)

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	require.Equal(t, domain.RepoKindMonorepo, repo.Kind)
	require.ElementsMatch(t, []domain.RepoSubProject{
		{Path: "apps/api", Kind: domain.RepoKindBackend},
		{Path: "apps/web", Kind: domain.RepoKindFrontend},
	}, repo.SubProjects)
	require.ElementsMatch(t, []string{domain.RepoKindBackend, domain.RepoKindFrontend}, repo.SubRepoKinds)
	require.Len(t, repos.subProjectWrites, 1)
}

func TestServiceOpen_DoesNotDetectWhenKindIsExplicit(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := monorepoLayout(t)

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root, Kind: domain.RepoKindBackend})
	require.NoError(t, err)

	require.Equal(t, domain.RepoKindBackend, repo.Kind)
	require.Empty(t, repo.SubProjects)
	require.Empty(t, repos.subProjectWrites)
}

func TestServiceOpen_DoesNotDetectForANonMonorepo(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/api\n"), 0o644))

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	require.Equal(t, domain.RepoKindBackend, repo.Kind)
	require.Empty(t, repo.SubProjects)
	require.Empty(t, repos.subProjectWrites)
}

func TestServiceOpen_DetectsAndPersistsTheMobilePlatform(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pubspec.yaml"),
		[]byte("name: app\ndependencies:\n  flutter:\n    sdk: flutter\n"), 0o644))

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	require.Equal(t, domain.RepoKindMobile, repo.Kind)
	require.Equal(t, domain.MobilePlatformCross, repo.MobilePlatform)
	require.Equal(t, []string{domain.MobilePlatformCross}, repos.mobilePlatformWrites)
}

func TestServiceOpen_DetectsThePlatformForAnExplicitMobileKind(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Package.swift"), []byte("// swift-tools-version:5.9\n"), 0o644))

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root, Kind: domain.RepoKindMobile})
	require.NoError(t, err)

	require.Equal(t, domain.MobilePlatformIOS, repo.MobilePlatform)
	require.Equal(t, []string{domain.MobilePlatformIOS}, repos.mobilePlatformWrites)
}

func TestServiceOpen_WritesNoPlatformForANonMobileRepo(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/api\n"), 0o644))

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	require.Empty(t, repo.MobilePlatform)
	require.Empty(t, repos.mobilePlatformWrites)
	require.Empty(t, repos.appIdentityWrites)
}

func TestServiceOpen_DetectsAndPersistsTheAppIdentity(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	for rel, body := range map[string]string{
		"pubspec.yaml":             "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n",
		"android/app/build.gradle": "android {\n    defaultConfig {\n        applicationId \"com.acme.app\"\n    }\n}\n",
		"ios/Runner.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n" +
			"\t\t\tPRODUCT_BUNDLE_IDENTIFIER = com.acme.app;\n" +
			"\t\t\tPRODUCT_BUNDLE_IDENTIFIER = com.acme.app.RunnerTests;\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	want := domain.AppIdentity{BundleID: "com.acme.app", PackageName: "com.acme.app"}
	require.Equal(t, want, repo.DetectedAppIdentity)
	require.Equal(t, []domain.AppIdentity{want}, repos.appIdentityWrites)
}

func TestServiceOpen_DetectsAndPersistsTheBuildTargets(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	for rel, body := range map[string]string{
		"pubspec.yaml":             "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n",
		"android/settings.gradle":  "include ':app'\n",
		"android/app/build.gradle": "plugins {\n    id 'com.android.application'\n}\n",
		"ios/Runner.xcodeproj/xcshareddata/xcschemes/Runner.xcscheme": "<Scheme></Scheme>",
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	want := domain.BuildTargets{XcodeScheme: "Runner", GradleModule: "app"}
	require.Equal(t, want, repo.DetectedBuildTargets)
	require.Equal(t, []domain.BuildTargets{want}, repos.buildTargetWrites)
}

func TestServiceOpen_WritesNoBuildTargetsWhenNothingIsReadable(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pubspec.yaml"),
		[]byte("name: app\ndependencies:\n  flutter:\n    sdk: flutter\n"), 0o644))

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	require.Equal(t, domain.RepoKindMobile, repo.Kind)
	require.Equal(t, domain.BuildTargets{}, repo.DetectedBuildTargets)
	require.Empty(t, repos.buildTargetWrites)
}

func TestServiceOpen_WritesNoAppIdentityWhenNothingIsReadable(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pubspec.yaml"),
		[]byte("name: app\ndependencies:\n  flutter:\n    sdk: flutter\n"), 0o644))

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	require.Equal(t, domain.RepoKindMobile, repo.Kind)
	require.Equal(t, domain.AppIdentity{}, repo.DetectedAppIdentity)
	require.Empty(t, repos.appIdentityWrites)
}
