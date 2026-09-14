package repository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// pbxproj renders the fragment of a project.pbxproj that matters here: one
// PRODUCT_BUNDLE_IDENTIFIER per build configuration, in file order.
func pbxproj(ids ...string) string {
	body := "// !$*UTF8*$!\n{\n\tobjects = {\n"
	for _, id := range ids {
		body += "\t\tbuildSettings = {\n\t\t\tPRODUCT_BUNDLE_IDENTIFIER = " + id + ";\n\t\t};\n"
	}
	return body + "\t};\n}\n"
}

func plist(bundleID string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>Runner</string>
	<key>CFBundleIdentifier</key>
	<string>` + bundleID + `</string>
</dict>
</plist>
`
}

func TestDetectAppIdentity(t *testing.T) {
	cases := []struct {
		name   string
		layout tree
		want   domain.AppIdentity
	}{
		{
			name:   "empty directory yields nothing",
			layout: tree{},
			want:   domain.AppIdentity{},
		},
		{
			name: "flutter app reports both platforms",
			layout: tree{
				"pubspec.yaml": "name: myapp\nflutter:\n  uses-material-design: true\n",
				"android/app/build.gradle": `android {
    defaultConfig {
        applicationId "com.acme.myapp"
        minSdkVersion 21
    }
}
`,
				"ios/Runner.xcodeproj/project.pbxproj": pbxproj("com.acme.myapp", "com.acme.myapp.RunnerTests"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.myapp", PackageName: "com.acme.myapp"},
		},
		{
			name: "kotlin dsl applicationId with an equals sign",
			layout: tree{
				"android/app/build.gradle.kts": "android {\n    defaultConfig {\n        applicationId = \"com.acme.kts\"\n    }\n}\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.kts"},
		},
		{
			name: "plain android repo keeps app/build.gradle at the root level",
			layout: tree{
				"settings.gradle":  "include ':app'\n",
				"app/build.gradle": "android {\n    defaultConfig {\n        applicationId 'com.acme.native'\n    }\n}\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.native"},
		},
		{
			name: "applicationIdSuffix is not an applicationId",
			layout: tree{
				"app/build.gradle": `android {
    buildTypes {
        debug {
            applicationIdSuffix ".debug"
        }
    }
}
`,
			},
			want: domain.AppIdentity{},
		},
		{
			name: "a commented-out applicationId is ignored",
			layout: tree{
				"app/build.gradle": "android {\n    // applicationId \"com.acme.old\"\n    defaultConfig {\n        applicationId \"com.acme.current\"\n    }\n}\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.current"},
		},
		{
			name: "an interpolated applicationId is refused rather than guessed",
			layout: tree{
				"app/build.gradle": "android {\n    defaultConfig {\n        applicationId \"com.acme.${flavor}\"\n    }\n}\n",
			},
			want: domain.AppIdentity{},
		},
		{
			name: "manifest package is the fallback when gradle names no applicationId",
			layout: tree{
				"app/build.gradle": "android {\n    namespace 'com.acme.lib'\n}\n",
				"app/src/main/AndroidManifest.xml": `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="com.acme.frommanifest">
    <application android:label="app" />
</manifest>
`,
			},
			want: domain.AppIdentity{PackageName: "com.acme.frommanifest"},
		},
		{
			name: "a queries <package> child is not the manifest's own package",
			layout: tree{
				"app/src/main/AndroidManifest.xml": `<manifest xmlns:android="http://schemas.android.com/apk/res/android">
    <queries>
        <package android:name="com.other.app" />
    </queries>
</manifest>
`,
			},
			want: domain.AppIdentity{},
		},
		{
			name: "gradle applicationId wins over the manifest package",
			layout: tree{
				"android/app/build.gradle":                      "android {\n    defaultConfig {\n        applicationId \"com.acme.shipped\"\n    }\n}\n",
				"android/app/src/main/AndroidManifest.xml":      `<manifest package="com.acme.namespace" />`,
				"android/app/src/debug/AndroidManifest.xml":     `<manifest package="com.acme.debug" />`,
				"android/app/src/profile/AndroidManifest.xml":   `<manifest package="com.acme.profile" />`,
				"android/gradle/wrapper/gradle-wrapper.propert": "distributionUrl=x\n",
			},
			want: domain.AppIdentity{PackageName: "com.acme.shipped"},
		},
		{
			name: "native ios project at the root, quoted bundle id",
			layout: tree{
				"MyApp.xcodeproj/project.pbxproj": pbxproj(`"com.acme.ios"`),
			},
			want: domain.AppIdentity{BundleID: "com.acme.ios"},
		},
		{
			name: "test and extension targets never win over the app",
			layout: tree{
				"ios/Runner.xcodeproj/project.pbxproj": pbxproj(
					"com.acme.app.RunnerTests",
					"com.acme.app.RunnerUITests",
					"com.acme.app.NotificationService",
					"com.acme.app",
					"com.acme.app.watchkitapp",
				),
			},
			want: domain.AppIdentity{BundleID: "com.acme.app"},
		},
		{
			name: "an independently named widget target does not win either",
			layout: tree{
				"MyApp.xcodeproj/project.pbxproj": pbxproj("com.acme.MyAppWidgetExtension", "com.acme.myapp"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.myapp"},
		},
		{
			name: "a pbxproj that only names variables falls through to Info.plist",
			layout: tree{
				"ios/Runner.xcodeproj/project.pbxproj": pbxproj("$(PRODUCT_BUNDLE_IDENTIFIER)", "$(PRODUCT_BUNDLE_IDENTIFIER).RunnerTests"),
				"ios/Runner/Info.plist":                plist("com.acme.fromplist"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.fromplist"},
		},
		{
			name: "a placeholder Info.plist yields nothing rather than the variable",
			layout: tree{
				"ios/Runner/Info.plist": plist("$(PRODUCT_BUNDLE_IDENTIFIER)"),
			},
			want: domain.AppIdentity{},
		},
		{
			name: "an Info.plist in a native app's target folder is found",
			layout: tree{
				"MyApp/Info.plist": plist("com.acme.nativeplist"),
			},
			want: domain.AppIdentity{BundleID: "com.acme.nativeplist"},
		},
		{
			name: "an unparseable pbxproj is not an error, just no answer",
			layout: tree{
				"ios/Runner.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n{ truncated",
			},
			want: domain.AppIdentity{},
		},
		{
			name: "a bare word is not an identifier",
			layout: tree{
				"app/build.gradle":              "android {\n    defaultConfig {\n        applicationId \"myapp\"\n    }\n}\n",
				"App.xcodeproj/project.pbxproj": pbxproj("Runner"),
			},
			want: domain.AppIdentity{},
		},
		{
			name: "a backend gradle project claims no package name",
			layout: tree{
				"build.gradle": "plugins { id 'java' }\ndependencies { implementation 'org.springframework:spring-core' }\n",
			},
			want: domain.AppIdentity{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, repository.DetectAppIdentity(materialise(t, tc.layout)))
		})
	}
}

func TestDetectAppIdentityHandlesUnreadableRoots(t *testing.T) {
	require.Equal(t, domain.AppIdentity{}, repository.DetectAppIdentity(""))
	require.Equal(t, domain.AppIdentity{}, repository.DetectAppIdentity("   "))
	require.Equal(t, domain.AppIdentity{}, repository.DetectAppIdentity("/nonexistent/path/that/is/not/there"))
}

// A monorepo's mobile sub-project carries its own identifiers, read from its
// own directory rather than from the repository root.
func TestDetectRepoSubProjectsCarriesTheAppIdentity(t *testing.T) {
	root := materialise(t, tree{
		"apps/api/go.mod":                                  "module example.com/api\n",
		"apps/mobile/pubspec.yaml":                         "name: app\nflutter:\n  sdk: flutter\n",
		"apps/mobile/android/app/build.gradle":             "android {\n    defaultConfig {\n        applicationId \"com.acme.sub\"\n    }\n}\n",
		"apps/mobile/ios/Runner.xcodeproj/project.pbxproj": pbxproj("com.acme.sub"),
	})

	subs := repository.DetectRepoSubProjects(root)
	require.NotEmpty(t, subs)

	byPath := map[string]domain.RepoSubProject{}
	for _, sp := range subs {
		byPath[sp.Path] = sp
	}
	mobile, ok := byPath["apps/mobile"]
	require.True(t, ok, "expected the mobile sub-project to be detected: %+v", subs)
	require.Equal(t, domain.RepoKindMobile, mobile.Kind)
	require.Equal(t, domain.AppIdentity{BundleID: "com.acme.sub", PackageName: "com.acme.sub"}, mobile.DetectedAppIdentity)

	api, ok := byPath["apps/api"]
	require.True(t, ok)
	require.Equal(t, domain.AppIdentity{}, api.DetectedAppIdentity, "a non-mobile sub-project claims no store identity")
}
