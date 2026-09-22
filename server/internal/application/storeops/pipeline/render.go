package pipeline

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const scriptMode uint32 = 0o755

const workflowMode uint32 = 0o644

var (
	identifierRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
	targetRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,99}$`)

	moduleRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,49}(:[A-Za-z0-9][A-Za-z0-9._-]{0,49}){0,4}$`)
	subPathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
)

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

func (s Spec) scriptPath() string {
	return path.Join(s.SubProjectPath, "scripts", "mobile-release.sh")
}

func (s Spec) workflowPath() string {
	name := "mobile-release"
	if s.SubProjectPath != "" {
		name += "-" + strings.ReplaceAll(s.SubProjectPath, "/", "-")
	}
	return ".github/workflows/" + name + ".yml"
}

func (s Spec) runFromRoot() string {
	return s.scriptPath()
}
