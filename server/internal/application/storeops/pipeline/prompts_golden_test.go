package pipeline

import "testing"

// TestGolden_PipelineValidationWording pins the exact byte output of
// render.go's validation refusals: they reach an agent as the build.Error
// a batch/store deploy leaves on ReleaseStoreBuild, read back by
// get_release/watch_release.
func TestGolden_PipelineValidationWording(t *testing.T) {
	assert := func(got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}

	assert(unsupportedPlatformKey.Render(pipelinePlatformInput{Platform: `"web"`}),
		`"web" is not a store platform this generator ships`)
	assert(invalidIdentifierKey.Render(pipelineIdentifierInput{Identifier: `"bad id"`}),
		`"bad id" is not a bundle id or package name (it reaches a shell word and a concurrency group)`)
	assert(invalidSubPathKey.Render(pipelinePathInput{Path: `"../x"`}),
		`"../x" is not a path inside the repository`)
	assert(iosSchemeRequiredKey.Render(pipelineSchemeInput{Scheme: `""`}),
		`iOS needs an Xcode scheme to archive, got ""`)
	assert(androidModuleRequiredKey.Render(pipelineModuleInput{Module: `""`}),
		`Android needs the Gradle module that produces the bundle, got ""`)
}
