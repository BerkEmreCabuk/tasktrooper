package repository_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// tree describes a repository layout as relative path -> file content. A path
// ending in "/" is created as a directory.
type tree map[string]string

func materialise(t *testing.T, layout tree) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range layout {
		full := filepath.Join(root, filepath.FromSlash(path))
		if len(path) > 0 && path[len(path)-1] == '/' {
			require.NoError(t, os.MkdirAll(full, 0o755))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

func TestDetectRepoKind(t *testing.T) {
	cases := []struct {
		name   string
		layout tree
		want   string
	}{
		{
			name:   "empty directory falls back to backend",
			layout: tree{},
			want:   domain.RepoKindBackend,
		},
		{
			name:   "go module is a backend",
			layout: tree{"go.mod": "module example.com/api\n"},
			want:   domain.RepoKindBackend,
		},
		{
			name:   "maven project is a backend",
			layout: tree{"pom.xml": "<project/>"},
			want:   domain.RepoKindBackend,
		},
		{
			name:   "python project is a backend",
			layout: tree{"pyproject.toml": "[project]\nname='svc'\n"},
			want:   domain.RepoKindBackend,
		},
		{
			name:   "node service without a web framework is a backend",
			layout: tree{"package.json": `{"dependencies":{"express":"^4.0.0","pg":"^8.0.0"}}`},
			want:   domain.RepoKindBackend,
		},
		{
			name:   "react app is a frontend",
			layout: tree{"package.json": `{"dependencies":{"react":"^18.0.0","react-dom":"^18.0.0"}}`},
			want:   domain.RepoKindFrontend,
		},
		{
			name:   "vite in devDependencies is a frontend",
			layout: tree{"package.json": `{"devDependencies":{"vite":"^5.0.0"}}`},
			want:   domain.RepoKindFrontend,
		},
		{
			name:   "scoped framework plugin still reads as a frontend",
			layout: tree{"package.json": `{"devDependencies":{"@vitejs/plugin-react":"^4.0.0"}}`},
			want:   domain.RepoKindFrontend,
		},
		{
			name:   "next app is a frontend",
			layout: tree{"package.json": `{"dependencies":{"next":"14.0.0"}}`},
			want:   domain.RepoKindFrontend,
		},
		{
			name:   "unparseable package.json does not crash the guess",
			layout: tree{"package.json": "{not json"},
			want:   domain.RepoKindBackend,
		},
		{
			name:   "flutter app is mobile",
			layout: tree{"pubspec.yaml": "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n"},
			want:   domain.RepoKindMobile,
		},
		{
			name:   "dart package without flutter is not mobile",
			layout: tree{"pubspec.yaml": "name: lib\ndependencies:\n  meta: ^1.0.0\n", "go.mod": "module x\n"},
			want:   domain.RepoKindBackend,
		},
		{
			name:   "xcode project is mobile",
			layout: tree{"MyApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!"},
			want:   domain.RepoKindMobile,
		},
		{
			name:   "xcodegen spec is mobile",
			layout: tree{"project.yml": "name: MyApp\n"},
			want:   domain.RepoKindMobile,
		},
		{
			name:   "swift package manifest is mobile",
			layout: tree{"Package.swift": "// swift-tools-version:5.9"},
			want:   domain.RepoKindMobile,
		},
		{
			name:   "android and ios folders together are mobile",
			layout: tree{"android/": "", "ios/": "", "package.json": `{"dependencies":{"react-native":"0.73.0"}}`},
			want:   domain.RepoKindMobile,
		},
		{
			name:   "a native android app is mobile",
			layout: tree{"settings.gradle.kts": "rootProject.name = \"app\"\n", "app/src/main/AndroidManifest.xml": "<manifest/>"},
			want:   domain.RepoKindMobile,
		},
		{
			name:   "a gradle backend without a manifest is still a backend",
			layout: tree{"build.gradle": "plugins { id 'java' }\n"},
			want:   domain.RepoKindBackend,
		},
		{
			name: "several projects under apps make a monorepo",
			layout: tree{
				"apps/backend/go.mod":     "module example.com/api\n",
				"apps/web/package.json":   `{"dependencies":{"react":"^18.0.0"}}`,
				"apps/mobile/project.yml": "name: MyApp\n",
			},
			want: domain.RepoKindMonorepo,
		},
		{
			name: "shared packages plus one app make a monorepo",
			layout: tree{
				"apps/api/go.mod":          "module example.com/api\n",
				"packages/ui/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
			},
			want: domain.RepoKindMonorepo,
		},
		{
			name: "a single project under apps is not a monorepo on its own",
			layout: tree{
				"apps/api/go.mod": "module example.com/api\n",
			},
			want: domain.RepoKindBackend,
		},
		{
			name: "backend and frontend at the root, no apps/packages convention, is a monorepo",
			layout: tree{
				"backend/go.mod":        "module example.com/api\n",
				"frontend/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
			},
			want: domain.RepoKindMonorepo,
		},
		{
			name: "go module at the root with a nested web package is a monorepo",
			layout: tree{
				"go.mod":           "module example.com/api\n",
				"web/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
			},
			want: domain.RepoKindMonorepo,
		},
		{
			name: "a go module and a package.json in the same directory is not a monorepo",
			layout: tree{
				"go.mod":       "module example.com/api\n",
				"package.json": `{"devDependencies":{"prettier":"^3.0.0"}}`,
			},
			want: domain.RepoKindBackend,
		},
		{
			name: "vendored copies do not fake a monorepo",
			layout: tree{
				"go.mod":                             "module example.com/api\n",
				"node_modules/left-pad/package.json": `{"name":"left-pad"}`,
				"vendor/dep/package.json":            `{"name":"dep"}`,
			},
			want: domain.RepoKindBackend,
		},
		{
			name: "mobile wins over monorepo when both could match",
			layout: tree{
				"pubspec.yaml":          "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n",
				"apps/api/go.mod":       "module example.com/api\n",
				"apps/web/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
			},
			want: domain.RepoKindMobile,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := repository.DetectRepoKind(materialise(t, tc.layout))
			require.Equal(t, tc.want, got)
			require.True(t, domain.ValidRepoKind(got))
		})
	}
}

func TestDetectRepoKindMissingPath(t *testing.T) {
	require.Equal(t, domain.RepoKindBackend, repository.DetectRepoKind(""))
	require.Equal(t, domain.RepoKindBackend, repository.DetectRepoKind(filepath.Join(t.TempDir(), "nope")))
}

func TestDetectRepoSubProjects(t *testing.T) {
	t.Run("classifies each app under apps/ by its own markers", func(t *testing.T) {
		root := materialise(t, tree{
			"apps/backend/go.mod":   "module example.com/api\n",
			"apps/web/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
		})
		got := repository.DetectRepoSubProjects(root)
		require.ElementsMatch(t, []domain.RepoSubProject{
			{Path: "apps/backend", Kind: domain.RepoKindBackend},
			{Path: "apps/web", Kind: domain.RepoKindFrontend},
		}, got)
	})

	t.Run("backend/frontend at the root with no apps/packages convention", func(t *testing.T) {
		root := materialise(t, tree{
			"backend/go.mod":        "module example.com/api\n",
			"frontend/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
		})
		got := repository.DetectRepoSubProjects(root)
		require.ElementsMatch(t, []domain.RepoSubProject{
			{Path: "backend", Kind: domain.RepoKindBackend},
			{Path: "frontend", Kind: domain.RepoKindFrontend},
		}, got)
	})

	t.Run("a go-root plus nested web package yields a root row", func(t *testing.T) {
		root := materialise(t, tree{
			"go.mod":           "module example.com/api\n",
			"web/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
		})
		got := repository.DetectRepoSubProjects(root)
		require.ElementsMatch(t, []domain.RepoSubProject{
			{Path: ".", Kind: domain.RepoKindBackend},
			{Path: "web", Kind: domain.RepoKindFrontend},
		}, got)
	})

	t.Run("not a monorepo returns nil", func(t *testing.T) {
		root := materialise(t, tree{"apps/api/go.mod": "module example.com/api\n"})
		require.Nil(t, repository.DetectRepoSubProjects(root))
	})

	t.Run("a marker-less packages child is not a sub-project", func(t *testing.T) {
		root := materialise(t, tree{
			"apps/api/go.mod":            "module example.com/api\n",
			"apps/web/package.json":      `{"dependencies":{"react":"^18.0.0"}}`,
			"packages/tsconfig/base.txt": "shared config, no ecosystem marker\n",
		})
		got := repository.DetectRepoSubProjects(root)
		require.ElementsMatch(t, []domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend},
			{Path: "apps/web", Kind: domain.RepoKindFrontend},
		}, got)
	})

	t.Run("a mobile sub-project is classified as mobile, with its platform", func(t *testing.T) {
		root := materialise(t, tree{
			"apps/api/go.mod":           "module example.com/api\n",
			"apps/mobile/Package.swift": "// swift-tools-version:5.9\n",
		})
		got := repository.DetectRepoSubProjects(root)
		require.ElementsMatch(t, []domain.RepoSubProject{
			{Path: "apps/api", Kind: domain.RepoKindBackend},
			{Path: "apps/mobile", Kind: domain.RepoKindMobile, MobilePlatform: domain.MobilePlatformIOS},
		}, got)
	})

	t.Run("a non-mobile sub-project carries no platform", func(t *testing.T) {
		root := materialise(t, tree{
			"apps/api/go.mod":       "module example.com/api\n",
			"apps/api/build.gradle": "// not android, and not mobile either\n",
			"apps/web/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
		})
		for _, sp := range repository.DetectRepoSubProjects(root) {
			require.Empty(t, sp.MobilePlatform, sp.Path)
		}
	})
}

func TestDetectMobilePlatform(t *testing.T) {
	cases := []struct {
		name   string
		layout tree
		want   string
	}{
		{
			name:   "nothing recognisable",
			layout: tree{"README.md": "hi\n"},
			want:   "",
		},
		{
			name:   "flutter app is cross platform",
			layout: tree{"pubspec.yaml": "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n"},
			want:   domain.MobilePlatformCross,
		},
		{
			name:   "react native layout is cross platform",
			layout: tree{"android/": "", "ios/": "", "package.json": `{"dependencies":{"react-native":"0.73.0"}}`},
			want:   domain.MobilePlatformCross,
		},
		{
			name:   "xcode project alone is ios",
			layout: tree{"MyApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "xcode workspace alone is ios",
			layout: tree{"MyApp.xcworkspace/contents.xcworkspacedata": "<Workspace/>"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "xcodegen spec alone is ios",
			layout: tree{"project.yml": "name: MyApp\n"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "swift package manifest alone is ios",
			layout: tree{"Package.swift": "// swift-tools-version:5.9"},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "gradle wrapper and manifest alone is android",
			layout: tree{"gradlew": "#!/bin/sh\n", "app/src/main/AndroidManifest.xml": "<manifest/>"},
			want:   domain.MobilePlatformAndroid,
		},
		{
			name:   "settings.gradle.kts alone is android",
			layout: tree{"settings.gradle.kts": `rootProject.name = "app"`},
			want:   domain.MobilePlatformAndroid,
		},
		{
			name:   "an ios folder without an android one is ios",
			layout: tree{"ios/": "", "package.json": `{"name":"app"}`},
			want:   domain.MobilePlatformIOS,
		},
		{
			name:   "both native toolchains in one tree is cross platform",
			layout: tree{"MyApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!", "build.gradle": "// android\n"},
			want:   domain.MobilePlatformCross,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := repository.DetectMobilePlatform(materialise(t, tc.layout))
			require.Equal(t, tc.want, got)
			if got != "" {
				require.True(t, domain.ValidMobilePlatform(got))
			}
		})
	}
}

func TestDetectMobilePlatformMissingPath(t *testing.T) {
	require.Empty(t, repository.DetectMobilePlatform(""))
	require.Empty(t, repository.DetectMobilePlatform(filepath.Join(t.TempDir(), "nope")))
}

func TestDetectDirectoryKindReportsThePlatform(t *testing.T) {
	root := materialise(t, tree{
		"apps/mobile/gradlew":                          "#!/bin/sh\n",
		"apps/mobile/app/src/main/AndroidManifest.xml": "<manifest/>",
		"apps/api/go.mod":                              "module example.com/api\n",
	})
	kind, platform := repository.DetectDirectoryKind(root, "apps/mobile")
	require.Equal(t, domain.RepoKindMobile, kind)
	require.Equal(t, domain.MobilePlatformAndroid, platform)

	kind, platform = repository.DetectDirectoryKind(root, "apps/api")
	require.Equal(t, domain.RepoKindBackend, kind)
	require.Empty(t, platform)
}
