package board

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveVerifyStages_GoProject(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	stages := ResolveVerifyStages(dir, domain.Repository{})
	if len(stages) != 2 || stages[0].Command[0] != "go" || stages[1].Command[1] != "vet" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func TestResolveVerifyStages_OverrideWins(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	stages := ResolveVerifyStages(dir, domain.Repository{VerifyCommand: "make check"})
	if len(stages) != 1 || stages[0].Command[0] != "make" || stages[0].Command[1] != "check" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func TestResolveVerifyStages_Rust(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "Cargo.toml")
	stages := ResolveVerifyStages(dir, domain.Repository{})
	if len(stages) != 1 || stages[0].Command[0] != "cargo" || stages[0].Command[1] != "check" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func TestResolveVerifyStages_Python(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "pyproject.toml")
	stages := ResolveVerifyStages(dir, domain.Repository{})
	if len(stages) != 1 || stages[0].Command[0] != "python" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func writeNPMProject(t *testing.T, dir string) {
	t.Helper()
	pkg := `{"scripts":{"build":"next build","test":"vitest run"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveVerifyStages_InstallsNPMDepsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	writeNPMProject(t, dir)
	stages := ResolveVerifyStages(dir, domain.Repository{})
	if len(stages) != 2 {
		t.Fatalf("expected install + build, got %+v", stages)
	}
	if !stages[0].Setup || stages[0].Command[1] != "install" {
		t.Fatalf("first stage must be the dependency install: %+v", stages[0])
	}
	if stages[0].Timeout <= 0 {
		t.Error("install stage needs its own timeout")
	}
	if stages[1].Command[2] != "build" {
		t.Fatalf("second stage must be the build: %+v", stages[1])
	}
}

func TestResolveVerifyStages_UsesNPMCIWithLockfile(t *testing.T) {
	dir := t.TempDir()
	writeNPMProject(t, dir)
	touch(t, dir, "package-lock.json")
	stages := ResolveVerifyStages(dir, domain.Repository{})
	if stages[0].Command[1] != "ci" {
		t.Fatalf("a lockfile must select npm ci: %+v", stages[0])
	}
}

func TestResolveVerifyStages_NPMBuildWithNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeNPMProject(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	stages := ResolveVerifyStages(dir, domain.Repository{})
	if len(stages) != 1 || stages[0].Command[0] != "npm" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}
