package localtoolchain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var envNames = map[string]string{
	"go":      "GOTOOLCHAIN",
	"node":    "NODE_VERSION",
	"python":  "PYTHON_VERSION",
	"ruby":    "RUBY_VERSION",
	"java":    "JAVA_VERSION",
	"rust":    "RUSTUP_TOOLCHAIN",
	"flutter": "FLUTTER_VERSION",
}

type Detector struct {
	root string
}

func New(workspaceRoot string) *Detector {
	return &Detector{root: strings.TrimSpace(workspaceRoot)}
}

func (d *Detector) Available() bool {
	return d != nil && d.root != ""
}

func (d *Detector) Detect(_ context.Context, workspace string) (port.Toolchain, error) {
	dir, err := d.resolve(workspace)
	if err != nil {
		return port.Toolchain{}, err
	}
	pins := toolchain.ReadPins(dir)
	out := port.Toolchain{
		Pins: make([]port.ToolchainPin, 0, len(pins)),
		Env:  map[string]string{},
	}
	for _, p := range pins {
		out.Pins = append(out.Pins, port.ToolchainPin{
			Language: p.Language, Version: p.Version, Exact: p.Exact, Source: p.Source,
		})
		name, ok := envNames[p.Language]
		if !p.Exact || !ok || !domain.SessionEnvAllowed(name) {
			continue
		}

		if _, taken := out.Env[name]; !taken {
			out.Env[name] = envValue(p.Language, p.Version)
		}
	}
	return out, nil
}

func (d *Detector) resolve(workspace string) (string, error) {
	if !d.Available() {
		return "", fmt.Errorf("toolchain detection has no workspace root")
	}
	if !filepath.IsAbs(workspace) {
		return "", fmt.Errorf("workspace %q is not an absolute path", workspace)
	}
	root, err := filepath.EvalSymlinks(d.root)
	if err != nil {
		return "", fmt.Errorf("workspace root: %w", err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", workspace, err)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace %q is not inside the workspace root", workspace)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace %q is not a directory", workspace)
	}
	return dir, nil
}

func envValue(language, version string) string {
	if language != "go" || strings.HasPrefix(version, "go") {
		return version
	}
	if strings.Count(version, ".") == 1 {
		version += ".0"
	}
	return "go" + version
}
