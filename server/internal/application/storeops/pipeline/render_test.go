package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func iosSpec() Spec {
	return Spec{
		Platform:   domain.MobileStorePlatformIOS,
		Identifier: "com.example.myapp",
		AppName:    "MyApp",
		StoreAppID: "6470000001",
		Scheme:     "MyApp",
	}
}

func androidSpec() Spec {
	return Spec{
		Platform:   domain.MobileStorePlatformAndroid,
		Identifier: "com.example.myapp",
		AppName:    "MyApp",
		Module:     "app",
	}
}

func render(t *testing.T, spec Spec) (script, workflow Artifact) {
	t.Helper()
	arts, err := Render(spec)
	require.NoError(t, err)
	require.Len(t, arts, 2)
	return arts[0], arts[1]
}

func codeOnly(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func funcBody(t *testing.T, script, name string) string {
	t.Helper()
	start := strings.Index(script, name+"() {")
	require.GreaterOrEqual(t, start, 0, "%s is not defined in the generated script", name)
	rest := script[start:]
	end := strings.Index(rest, "\n}\n")
	require.GreaterOrEqual(t, end, 0, "%s has no closing brace", name)
	return rest[:end]
}

func TestRenderReturnsTheScriptBeforeTheWorkflow(t *testing.T) {
	for _, spec := range []Spec{iosSpec(), androidSpec()} {
		script, workflow := render(t, spec)

		assert.Equal(t, "scripts/mobile-release.sh", script.Path)
		assert.Equal(t, ".github/workflows/mobile-release.yml", workflow.Path)

		assert.Equal(t, uint32(0o755), script.Mode)
		assert.Equal(t, uint32(0o644), workflow.Mode)
		assert.Contains(t, workflow.Body, script.Path,
			"the wrapper has to name the script it calls")
	}
}

func TestRenderScopesASubProject(t *testing.T) {
	spec := androidSpec()
	spec.SubProjectPath = "apps/mobile"
	script, workflow := render(t, spec)

	assert.Equal(t, "apps/mobile/scripts/mobile-release.sh", script.Path)
	assert.Equal(t, ".github/workflows/mobile-release-apps-mobile.yml", workflow.Path)
	assert.Contains(t, workflow.Body, "bash apps/mobile/scripts/mobile-release.sh")
	assert.Contains(t, workflow.Body, "path: apps/mobile/build/mobile-release/**")
}

func TestGeneratedScriptParses(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			script, _ := render(t, spec)
			path := filepath.Join(t.TempDir(), "mobile-release.sh")
			require.NoError(t, os.WriteFile(path, []byte(script.Body), 0o755))

			out, err := exec.Command(bash, "-n", path).CombinedOutput()
			require.NoError(t, err, "bash -n: %s", out)
		})
	}
}

func TestGeneratedScriptRefusesZsh(t *testing.T) {
	for _, spec := range []Spec{iosSpec(), androidSpec()} {
		script, _ := render(t, spec)
		assert.True(t, strings.HasPrefix(script.Body, "#!/usr/bin/env bash\n"))
		assert.Contains(t, script.Body, "set -euo pipefail")
		assert.Contains(t, script.Body, `if [ -z "${BASH_VERSION:-}" ]; then`)
	}
}

func TestEveryBase64SecretIsDecodedInTheScript(t *testing.T) {
	cases := map[string]struct {
		spec   Spec
		base64 []string
		plain  []string
	}{
		"ios": {
			spec:   iosSpec(),
			base64: []string{"IOS_DIST_CERT_P12", "IOS_PROFILE_B64", "ASC_KEY_P8"},
			plain:  []string{"IOS_CERT_PASSWORD", "ASC_KEY_ID", "ASC_ISSUER_ID"},
		},
		"android": {
			spec:   androidSpec(),
			base64: []string{"ANDROID_UPLOAD_KEYSTORE_B64", "PLAY_SA_JSON"},
			plain: []string{
				"ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			script, workflow := render(t, tc.spec)
			for _, secret := range tc.base64 {
				assert.Contains(t, script.Body, "decode_secret "+secret,
					"%s arrives base64-encoded and every consumer wants raw bytes", secret)
			}
			for _, secret := range append(tc.base64, tc.plain...) {
				assert.Contains(t, workflow.Body, secret+": ${{ secrets."+secret+" }}",
					"the wrapper's only job with a secret is to put it in the environment")
			}
			assert.NotContains(t, codeOnly(workflow.Body), "base64",
				"decoding is a step, and every step lives in the script")
		})
	}
}

func TestDecodedKeysAreMaskedInTheScriptAndOnlyUnderActions(t *testing.T) {
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			script, workflow := render(t, spec)
			assert.Contains(t, script.Body, `echo "::add-mask::$line"`)
			assert.Contains(t, script.Body, `if [ -z "${GITHUB_ACTIONS:-}" ]; then`)
			assert.NotContains(t, codeOnly(workflow.Body), "add-mask")
		})
	}
	iosScript, _ := render(t, iosSpec())
	assert.Contains(t, funcBody(t, iosScript.Body, "asc_key"), `mask_lines < "$ASC_KEY_PATH"`,
		"the ASC key is the one the templates called out by name")
}

func TestProdDoesNotRebuild(t *testing.T) {
	t.Run("ios", func(t *testing.T) {
		script, _ := render(t, iosSpec())
		prod := codeOnly(funcBody(t, script.Body, "release_prod"))
		assert.NotContains(t, prod, "archive")
		assert.NotContains(t, prod, "altool")
		assert.NotContains(t, prod, "resolve_build_number")
		assert.Contains(t, prod, "--skip_binary_upload true")
		assert.Contains(t, prod, "--submit_for_review")

		stage := funcBody(t, script.Body, "release_stage")
		assert.Contains(t, stage, "xcodebuild -scheme")
		assert.Contains(t, stage, "archive")
		assert.Contains(t, stage, "-exportArchive")
	})

	t.Run("android", func(t *testing.T) {
		script, _ := render(t, androidSpec())
		prod := codeOnly(funcBody(t, script.Body, "release_prod"))
		assert.NotContains(t, prod, "bundleRelease")
		assert.NotContains(t, prod, "gradlew")
		assert.NotContains(t, prod, "resolve_build_number")
		assert.Contains(t, prod, "track_promote_to:production")

		stage := funcBody(t, script.Body, "release_stage")
		assert.Contains(t, stage, `":$MODULE:bundleRelease"`)
	})
}

func TestScriptCarriesTheWholeProcedure(t *testing.T) {
	t.Run("ios", func(t *testing.T) {
		script, _ := render(t, iosSpec())
		stage := funcBody(t, script.Body, "release_stage")
		for _, want := range []string{

			`sec "create-keychain`,
			`sec "import`,

			`sec "set-key-partition-list`,
			"Provisioning Profiles",
			"CODE_SIGN_STYLE=Manual",
			`PRODUCT_BUNDLE_IDENTIFIER="$IDENTIFIER"`,
			`CURRENT_PROJECT_VERSION="$BUILD_NUMBER"`,
			"app-store-connect",
			"xcrun altool --upload-app",
		} {
			assert.Contains(t, stage, want)
		}
	})

	t.Run("android", func(t *testing.T) {
		script, _ := render(t, androidSpec())
		stage := funcBody(t, script.Body, "release_stage")
		for _, want := range []string{
			"decode_secret ANDROID_UPLOAD_KEYSTORE_B64",

			`signing_properties "$keystore"`,
			`-g "$GRADLE_RUN_HOME"`,
			`-PversionCode="$BUILD_NUMBER"`,
			"track:internal",
		} {
			assert.Contains(t, stage, want)
		}
	})
}

func TestSigningValuesNeverReachArgv(t *testing.T) {
	t.Run("android", func(t *testing.T) {
		script, _ := render(t, androidSpec())
		for _, forbidden := range []string{
			"-Pandroid.injected.signing.store.password=",
			"-Pandroid.injected.signing.key.password=",
			"-Pandroid.injected.signing.key.alias=",
		} {
			assert.NotContains(t, script.Body, forbidden)
		}
	})

	t.Run("ios", func(t *testing.T) {
		script, _ := render(t, iosSpec())
		for _, forbidden := range []string{
			`security import "$WORKDIR/dist.p12"`,
			`security create-keychain -p`,
			`security set-key-partition-list`,
		} {
			assert.NotContains(t, script.Body, forbidden)
		}
	})
}

func TestPlayFirstUploadStopsCleanlyWithTheArtifact(t *testing.T) {
	script, workflow := render(t, androidSpec())
	stage := funcBody(t, script.Body, "release_stage")

	copyAt := strings.Index(stage, `cp "$built" "$AAB"`)
	uploadAt := strings.Index(stage, "fastlane run supply")
	require.GreaterOrEqual(t, copyAt, 0)
	require.GreaterOrEqual(t, uploadAt, 0)
	assert.Less(t, copyAt, uploadAt, "a refused upload must still leave a signed bundle")

	assert.Contains(t, script.Body, `UPLOAD="${MOBILE_RELEASE_UPLOAD:-true}"`,
		"the deliberate opt-out for the first run")
	assert.Contains(t, stage, `if [ "$UPLOAD" != "true" ]; then`)
	assert.Contains(t, stage, "cannot go up over the API",
		"and the same explanation when Play refuses it rather than being told")
	assert.Contains(t, workflow.Body, "MOBILE_RELEASE_UPLOAD: ${{ inputs.upload }}")
	assert.Contains(t, workflow.Body, "if-no-files-found: ignore")
}

func TestWorkflowIsOnlyAWrapper(t *testing.T) {
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			_, workflow := render(t, spec)
			steps := codeOnly(workflow.Body)
			for _, forbidden := range []string{
				"xcodebuild", "gradlew", "altool", "security import",
				"base64", "deliver", "supply", "import-codesign-certs",
				"upload-google-play", "upload-testflight-build",
			} {
				assert.NotContains(t, steps, forbidden,
					"%q is a release step and belongs in the script", forbidden)
			}
			assert.Contains(t, workflow.Body, "workflow_dispatch:")
			assert.Contains(t, workflow.Body, "actions/checkout@v4")
			assert.Contains(t, workflow.Body, `run: bash scripts/mobile-release.sh "$CHANNEL"`)
		})
	}
}

func TestWorkflowRunsOnTheRightMachineAndSerialisesPerApp(t *testing.T) {
	_, ios := render(t, iosSpec())
	assert.Contains(t, ios.Body, "runs-on: macos-14", "Apple's toolchain exists nowhere else")

	_, android := render(t, androidSpec())
	assert.Contains(t, android.Body, "runs-on: ubuntu-latest")
	assert.Contains(t, android.Body, "actions/setup-java@v4")

	for _, workflow := range []Artifact{ios, android} {

		assert.Contains(t, workflow.Body, "group: mobile-${{ inputs.channel }}-com.example.myapp")
		assert.Contains(t, workflow.Body, "cancel-in-progress: false")
	}
}

func TestRenderKeepsANestedGradleModule(t *testing.T) {
	artifacts, err := Render(Spec{
		Platform:   domain.MobileStorePlatformAndroid,
		Identifier: "com.example.app",
		Module:     "apps:android",
	})
	require.NoError(t, err)
	assert.Contains(t, artifacts[0].Body, "MODULE='apps:android'")
}

func TestRenderRefusesWhatItCannotSafelyInterpolate(t *testing.T) {
	cases := map[string]Spec{
		"no platform":      {Identifier: "com.example.app", Scheme: "App"},
		"unknown platform": {Platform: "web", Identifier: "com.example.app"},
		"no identifier":    {Platform: domain.MobileStorePlatformIOS, Scheme: "App"},
		"shell in the identifier": {
			Platform: domain.MobileStorePlatformIOS, Identifier: "com.example$(id)", Scheme: "App",
		},
		"ios without a scheme": {
			Platform: domain.MobileStorePlatformIOS, Identifier: "com.example.app",
		},
		"android without a module": {
			Platform: domain.MobileStorePlatformAndroid, Identifier: "com.example.app",
		},
		"escaping sub-project": {
			Platform: domain.MobileStorePlatformAndroid, Identifier: "com.example.app",
			Module: "app", SubProjectPath: "../../etc",
		},
		"shell in the module": {
			Platform: domain.MobileStorePlatformAndroid, Identifier: "com.example.app",
			Module: "app;rm -rf /",
		},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Render(spec)
			assert.Error(t, err)
		})
	}
}

func TestDisplayNameIsQuotedRatherThanRefused(t *testing.T) {
	spec := iosSpec()
	spec.AppName = "It's a Test\"App"
	script, _ := render(t, spec)
	assert.Contains(t, script.Body, `APP_NAME='It'\''s a Test"App'`)

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	path := filepath.Join(t.TempDir(), "mobile-release.sh")
	require.NoError(t, os.WriteFile(path, []byte(script.Body), 0o755))
	out, err := exec.Command(bash, "-n", path).CombinedOutput()
	require.NoError(t, err, "bash -n: %s", out)
}
