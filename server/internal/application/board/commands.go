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
		return []Stage{{Name: "verify", Command: cmd}}
	}
	return detectBuild(dir)
}
