package repository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const xcscheme = `<?xml version="1.0" encoding="UTF-8"?><Scheme LastUpgradeVersion="1500" version="1.7"></Scheme>`

const (
	appGradle = "plugins {\n    id 'com.android.application'\n}\n\nandroid {\n    namespace 'com.acme.app'\n}\n"
	libGradle = "plugins {\n    id 'com.android.library'\n}\n"
)

func TestDetectBuildTargets(t *testing.T) {
	cases := []struct {
		name   string
		layout tree
		want   domain.BuildTargets
	}{
		{
			name:   "empty directory states nothing",
			layout: tree{},
			want:   domain.BuildTargets{},
		},
		{
			name: "the one shared scheme is the answer",
			layout: tree{
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyApp.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{

			name: "a shared scheme unrelated to the project name still wins",
			layout: tree{
				"MyApp.xcodeproj/xcshareddata/xcschemes/Production.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{XcodeScheme: "Production"},
		},
		{
			name: "test and UI-test schemes are not the app",
			layout: tree{
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyApp.xcscheme":        xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyAppTests.xcscheme":   xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyAppUITests.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name: "an app extension scheme is not the app either",
			layout: tree{
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyApp.xcscheme":                xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/MyAppWidgetExtension.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{

			name: "two unrelated shared schemes leave it unanswered",
			layout: tree{
				"MyApp.xcodeproj/xcshareddata/xcschemes/Staging.xcscheme":    xcscheme,
				"MyApp.xcodeproj/xcshareddata/xcschemes/Production.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{},
		},
		{

			name: "the project name answers only when nothing is shared",
			layout: tree{
				"MyApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n{}\n",
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name: "a workspace wrapping its project is still one name",
			layout: tree{
				"MyApp.xcodeproj/project.pbxproj":            "// !$*UTF8*$!\n{}\n",
				"MyApp.xcworkspace/contents.xcworkspacedata": "<Workspace></Workspace>",
			},
			want: domain.BuildTargets{XcodeScheme: "MyApp"},
		},
		{
			name: "two unrelated projects and no shared scheme leave it unanswered",
			layout: tree{
				"MyApp.xcodeproj/project.pbxproj":    "// !$*UTF8*$!\n{}\n",
				"OtherApp.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n{}\n",
			},
			want: domain.BuildTargets{},
		},
		{
			name: "the single included Android module is the answer",
			layout: tree{
				"settings.gradle":  "rootProject.name = 'acme'\ninclude ':app'\n",
				"app/build.gradle": appGradle,
				"gradlew":          "#!/bin/sh\n",
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "library modules are not the thing that bundles",
			layout: tree{
				"settings.gradle":       "include ':app', ':core', ':data'\n",
				"app/build.gradle":      appGradle,
				"core/build.gradle":     libGradle,
				"data/build.gradle.kts": "plugins {\n    id(\"com.android.library\")\n}\n",
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{

			name: "two application modules leave it unanswered",
			layout: tree{
				"settings.gradle":   "include ':app'\ninclude ':wear'\n",
				"app/build.gradle":  appGradle,
				"wear/build.gradle": appGradle,
			},
			want: domain.BuildTargets{},
		},
		{
			name: "a module the settings file never includes is not in the build",
			layout: tree{
				"settings.gradle":     "include ':app'\n",
				"app/build.gradle":    appGradle,
				"sample/build.gradle": appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "a commented-out include is not an include",
			layout: tree{
				"settings.gradle":   "include ':app'\n// include ':wear'\n/* include ':tv' */\n",
				"app/build.gradle":  appGradle,
				"wear/build.gradle": appGradle,
				"tv/build.gradle":   appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "the Kotlin DSL, a multi-line include and a catalog alias",
			layout: tree{
				"settings.gradle.kts":         "rootProject.name = \"acme\"\ninclude(\n    \":androidApp\",\n    \":shared\",\n)\n",
				"androidApp/build.gradle.kts": "plugins {\n    alias(libs.plugins.android.application)\n}\n",
				"shared/build.gradle.kts":     "plugins {\n    alias(libs.plugins.android.library)\n}\n",
			},
			want: domain.BuildTargets{GradleModule: "androidApp"},
		},
		{

			name: "a nested module keeps its colons",
			layout: tree{
				"settings.gradle":           "include ':apps:android'\ninclude ':libs:core'\n",
				"apps/android/build.gradle": appGradle,
				"libs/core/build.gradle":    libGradle,
			},
			want: domain.BuildTargets{GradleModule: "apps:android"},
		},
		{
			name: "includeBuild is not include",
			layout: tree{
				"settings.gradle":          "includeBuild('build-logic')\ninclude ':app'\n",
				"app/build.gradle":         appGradle,
				"build-logic/build.gradle": appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{

			name: "the Flutter layout answers both halves",
			layout: tree{
				"pubspec.yaml": "name: acme\nflutter:\n  sdk: flutter\n",
				"ios/Runner.xcodeproj/xcshareddata/xcschemes/Runner.xcscheme": xcscheme,
				"ios/Runner.xcworkspace/contents.xcworkspacedata":             "<Workspace></Workspace>",
				"android/settings.gradle":                                     "include ':app'\n",
				"android/app/build.gradle":                                    appGradle,
			},
			want: domain.BuildTargets{XcodeScheme: "Runner", GradleModule: "app"},
		},
		{

			name: "one half can answer while the other stays empty",
			layout: tree{
				"android/settings.gradle":  "include ':app'\n",
				"android/app/build.gradle": appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{

			name: "the conventional app module answers without a settings file",
			layout: tree{
				"android/app/build.gradle": appGradle,
			},
			want: domain.BuildTargets{GradleModule: "app"},
		},
		{
			name: "a Gradle JVM backend is not an Android app",
			layout: tree{
				"settings.gradle":      "include ':service'\n",
				"service/build.gradle": "plugins {\n    id 'java'\n}\n",
			},
			want: domain.BuildTargets{},
		},
		{
			name: "a scheme name that would need shell escaping is not answered",
			layout: tree{
				"MyApp.xcodeproj/xcshareddata/xcschemes/My$App.xcscheme": xcscheme,
			},
			want: domain.BuildTargets{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, repository.DetectBuildTargets(materialise(t, tc.layout)))
		})
	}
}

func TestDetectBuildTargetsHandlesUnreadableRoots(t *testing.T) {
	require.Equal(t, domain.BuildTargets{}, repository.DetectBuildTargets(""))
	require.Equal(t, domain.BuildTargets{}, repository.DetectBuildTargets("   "))
	require.Equal(t, domain.BuildTargets{}, repository.DetectBuildTargets("/nonexistent/path/that/is/not/there"))
}

func TestDetectRepoSubProjectsCarriesTheBuildTargets(t *testing.T) {
	root := materialise(t, tree{
		"apps/api/go.mod":          "module example.com/api\n",
		"apps/mobile/pubspec.yaml": "name: app\nflutter:\n  sdk: flutter\n",
		"apps/mobile/ios/Runner.xcodeproj/xcshareddata/xcschemes/Runner.xcscheme": xcscheme,
		"apps/mobile/android/settings.gradle":                                     "include ':app'\n",
		"apps/mobile/android/app/build.gradle":                                    appGradle,
	})

	byPath := map[string]domain.RepoSubProject{}
	for _, sp := range repository.DetectRepoSubProjects(root) {
		byPath[sp.Path] = sp
	}

	mobile, ok := byPath["apps/mobile"]
	require.True(t, ok, "expected the mobile sub-project to be detected: %+v", byPath)
	require.Equal(t, domain.BuildTargets{XcodeScheme: "Runner", GradleModule: "app"}, mobile.DetectedBuildTargets)

	api, ok := byPath["apps/api"]
	require.True(t, ok)
	require.Equal(t, domain.BuildTargets{}, api.DetectedBuildTargets, "a non-mobile sub-project builds no release artifact")
}
