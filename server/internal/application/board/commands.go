package board

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Stage struct {
	Name    string
	Command []string
	Setup   bool
	Timeout time.Duration
}

func splitCommand(s string) []string { return strings.Fields(strings.TrimSpace(s)) }

func markerExists(dir string, names ...string) bool {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

func detectBuild(dir string) []Stage {
	var out []Stage
	switch {
	case markerExists(dir, "go.mod"):
		out = append(out,
			Stage{Name: "build", Command: []string{"go", "build", "./..."}},
			Stage{Name: "vet", Command: []string{"go", "vet", "./..."}})
	case markerExists(dir, "Cargo.toml"):
		out = append(out, Stage{Name: "build", Command: []string{"cargo", "check"}})
	case markerExists(dir, "pyproject.toml", "requirements.txt"):
		out = append(out, Stage{Name: "build", Command: []string{"python", "-m", "compileall", "."}})
	case markerExists(dir, "pom.xml"):
		out = append(out, Stage{Name: "build", Command: []string{"mvn", "-q", "compile"}})
	case markerExists(dir, "build.gradle", "build.gradle.kts"):
		out = append(out, Stage{Name: "build", Command: []string{"gradle", "build", "-x", "test"}})
	}
	if markerExists(dir, "pubspec.yaml") {
		out = append(out, Stage{Name: "analyze", Command: []string{"flutter", "analyze"}})
	}
	if hasNPMScript(dir, "build") {
		out = append(out, npmInstallStage(dir, "")...)
		out = append(out, Stage{Name: "build-web", Command: []string{"npm", "run", "build"}})
	}
	if webDir := filepath.Join(dir, "web"); hasNPMScript(webDir, "build") {
		out = append(out, npmInstallStage(webDir, "web")...)
		out = append(out, Stage{Name: "build-web", Command: []string{"npm", "run", "build", "--prefix", webDir}})
	}
	return out
}

const npmInstallDefaultTimeout = 15 * time.Minute

func npmInstallStage(dir, label string) []Stage {
	if hasNodeModules(dir) {
		return nil
	}
	cmd := []string{"npm", "install", "--no-audit", "--no-fund"}
	if markerExists(dir, "package-lock.json") {
		cmd = []string{"npm", "ci", "--no-audit", "--no-fund"}
	}
	if label != "" {
		cmd = append(cmd, "--prefix", dir)
	}
	name := "npm-install"
	if label != "" {
		name += "-" + label
	}
	return []Stage{{Name: name, Command: cmd, Setup: true, Timeout: npmInstallDefaultTimeout}}
}

func hasNodeModules(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "node_modules"))
	return err == nil && info.IsDir()
}

func ResolveVerifyStages(dir string, repo domain.Repository) []Stage {
	if cmd := splitCommand(repo.VerifyCommand); len(cmd) > 0 {
		// A declared command gets the dependency installs a detected one gets.
		// Without them `make lint` ran in a fresh task checkout with no
		// node_modules anywhere, `npx tsc` fetched an unrelated registry package
		// called tsc whose only output is an error, and the gate reported that
		// as the agent's build failure.
		return append(nodeSetupStages(dir), Stage{Name: "verify", Command: cmd})
	}
	return detectBuild(dir)
}

// nodeSetupMaxDepth bounds how far below the workspace root a package with its
// own lockfile is looked for, and nodeSetupMaxStages how many installs one
// verification may start.
const (
	nodeSetupMaxDepth  = 3
	nodeSetupMaxStages = 6
)

// skipSetupDirs are never searched for packages: dependency trees and build
// output hold package files that are not the project's own.
var skipSetupDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"out": true, "coverage": true, "target": true,
}

// nodeSetupStages returns an `npm ci` setup stage for every package in the
// workspace that has a package-lock.json but no node_modules yet: the root and
// nested ones such as desktop/ and desktop/ui/ in a monorepo. A package without
// a lockfile is left alone, because installing it would resolve versions the
// repository never pinned.
func nodeSetupStages(dir string) []Stage {
	var rels []string
	var walk func(abs, rel string, depth int)
	walk = func(abs, rel string, depth int) {
		if len(rels) >= nodeSetupMaxStages {
			return
		}
		if markerExists(abs, "package.json") && markerExists(abs, "package-lock.json") && !hasNodeModules(abs) {
			rels = append(rels, rel)
		}
		if depth >= nodeSetupMaxDepth {
			return
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			// IsDir is false for a symlink, which also keeps a link cycle out.
			if !e.IsDir() || strings.HasPrefix(name, ".") || skipSetupDirs[name] {
				continue
			}
			walk(filepath.Join(abs, name), filepath.Join(rel, name), depth+1)
		}
	}
	walk(dir, "", 0)
	var stages []Stage
	for _, rel := range rels {
		if rel == "" {
			stages = append(stages, npmInstallStage(dir, "")...)
			continue
		}
		stages = append(stages, npmInstallStage(filepath.Join(dir, rel), filepath.ToSlash(rel))...)
	}
	return stages
}
