// Package pipeline turns one repository's store binding into the files that
// actually ship it. There is exactly one generator because there is exactly
// one behaviour: the same script runs under GitHub Actions and on a paired
// local runner, and the workflow is a thin wrapper that calls it. Two
// independently maintained copies of a release procedure drift, and the one
// that drifts is always the one nobody watches.
package pipeline

// Spec is what the generated files need to know about the app they ship. It is
// derived from the store binding (the picked app) plus what the working copy
// states about itself, never from a template variable somebody typed twice.
type Spec struct {
	Platform   string // domain.MobileStorePlatformIOS / MobileStorePlatformAndroid
	Identifier string // bundle ID / package name
	AppName    string // display name from the store console
	StoreAppID string // ASC app resource id; "" for Play, which keys on Identifier

	// Scheme is the Xcode scheme to archive (iOS only). Module is the Gradle
	// module that produces the bundle (Android only, e.g. "app"). Each is ""
	// on the other platform.
	Scheme string
	Module string

	// SubProjectPath scopes the generated paths inside a monorepo. "" means
	// the repository root.
	SubProjectPath string
}

// Artifact is one generated file, addressed relative to the repository root.
// Mode matters: the release script is executed by both engines, so it has to
// land executable rather than being chmod'd by whichever caller remembers.
type Artifact struct {
	Path string
	Body string
	Mode uint32
}

// Render is implemented alongside this file. It returns, for one Spec, the
// release script and the workflow that calls it — in that order, so a caller
// writing them one at a time never has a workflow referencing a script that is
// not there yet.
//
//	func Render(spec Spec) ([]Artifact, error)
