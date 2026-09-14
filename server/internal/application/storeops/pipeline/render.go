package pipeline

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// scriptMode is the one mode the release script may land with. Both engines
// exec it, and a caller that chmod'd afterwards would be a second place the
// permission is decided.
const scriptMode uint32 = 0o755

// workflowMode is an ordinary file: Actions reads it, nothing runs it.
const workflowMode uint32 = 0o644

// The generated files interpolate these values into shell words and YAML
// scalars, and every one of them arrives from a store console or a working
// copy rather than from this repository. They are matched rather than escaped
// because an identifier that needs escaping is a mis-detected identifier, and
// shipping it quoted would only move the surprise to the build.
var (
	identifierRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
	targetRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,99}$`)
	// moduleRe is targetRe plus the colon and minus the space: a Gradle module
	// path is colon-separated (`apps:android` is the <module> in
	// :<module>:bundleRelease) and no segment of one may contain a space.
	moduleRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,49}(:[A-Za-z0-9][A-Za-z0-9._-]{0,49}){0,4}$`)
	subPathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
)

// Render turns one Spec into the release script and the workflow that calls
// it, in that order. See the contract in spec.go.
func Render(spec Spec) ([]Artifact, error) {
	spec, err := spec.normalize()
	if err != nil {
		return nil, err
	}

	script, workflow := "", ""
	switch spec.Platform {
	case domain.MobileStorePlatformIOS:
		script, workflow = iosScript(spec), iosWorkflow(spec)
	case domain.MobileStorePlatformAndroid:
		script, workflow = androidScript(spec), androidWorkflow(spec)
	default:
		return nil, fmt.Errorf("pipeline: %q is not a store platform this generator ships", spec.Platform)
	}

	return []Artifact{
		{Path: spec.scriptPath(), Body: script, Mode: scriptMode},
		{Path: spec.workflowPath(), Body: workflow, Mode: workflowMode},
	}, nil
}

func (s Spec) normalize() (Spec, error) {
	s.Platform = strings.ToLower(strings.TrimSpace(s.Platform))
	s.Identifier = strings.TrimSpace(s.Identifier)
	s.AppName = strings.TrimSpace(s.AppName)
	s.StoreAppID = strings.TrimSpace(s.StoreAppID)
	s.Scheme = strings.TrimSpace(s.Scheme)
	s.Module = strings.TrimSpace(s.Module)
	s.SubProjectPath = strings.Trim(strings.TrimSpace(s.SubProjectPath), "/")

	if !identifierRe.MatchString(s.Identifier) {
		return Spec{}, fmt.Errorf("pipeline: %q is not a bundle id or package name (it reaches a shell word and a concurrency group)", s.Identifier)
	}
	if s.SubProjectPath != "" {
		if !subPathRe.MatchString(s.SubProjectPath) || strings.Contains(s.SubProjectPath, "..") {
			return Spec{}, fmt.Errorf("pipeline: %q is not a path inside the repository", s.SubProjectPath)
		}
	}
	if s.AppName == "" {
		s.AppName = s.Identifier
	}

	switch s.Platform {
	case domain.MobileStorePlatformIOS:
		if !targetRe.MatchString(s.Scheme) {
			return Spec{}, fmt.Errorf("pipeline: iOS needs an Xcode scheme to archive, got %q", s.Scheme)
		}
	case domain.MobileStorePlatformAndroid:
		if !moduleRe.MatchString(s.Module) {
			return Spec{}, fmt.Errorf("pipeline: Android needs the Gradle module that produces the bundle, got %q", s.Module)
		}
	}
	return s, nil
}

// scriptPath scopes the script to the app inside a monorepo: it sits beside
// the thing it builds, because that is the directory gradlew and the Xcode
// project are resolved from.
func (s Spec) scriptPath() string {
	return path.Join(s.SubProjectPath, "scripts", "mobile-release.sh")
}

// workflowPath is always at the REPOSITORY root: GitHub reads
// `.github/workflows` there and nowhere else, so a sub-project's workflow
// cannot travel with its sub-project the way its script does. The name carries
// the sub-path instead — two apps in one monorepo would otherwise render the
// same file twice and the second would win silently.
func (s Spec) workflowPath() string {
	name := "mobile-release"
	if s.SubProjectPath != "" {
		name += "-" + strings.ReplaceAll(s.SubProjectPath, "/", "-")
	}
	return ".github/workflows/" + name + ".yml"
}

// runFromRoot is how the workflow, which always starts at the repository root,
// reaches a script that may not be there.
func (s Spec) runFromRoot() string {
	return s.scriptPath()
}
