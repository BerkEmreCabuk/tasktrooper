package toolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectGoToolchainDirective(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/x\n\ngo 1.23\n\ntoolchain go1.23.4\n")
	req := Detect(dir)
	if req.Go != "1.23.4" || !req.GoExact {
		t.Fatalf("got %+v, want exact 1.23.4", req)
	}
}

func TestDetectGoDirectiveOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/x\n\ngo 1.22\n")
	req := Detect(dir)
	if req.Go != "1.22" || req.GoExact {
		t.Fatalf("got %+v, want minimum 1.22", req)
	}
}

func TestDetectNodePrecedence(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"engines":{"node":">=20.11 <21"}}`)
	writeFile(t, dir, ".nvmrc", "v18.19.0\n")
	req := Detect(dir)
	if req.Node != "18.19.0" || req.NodeSource != ".nvmrc" {
		t.Fatalf("got %+v, want .nvmrc 18.19.0", req)
	}
}

func TestDetectEnginesRange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"engines":{"node":"^20.9.0"}}`)
	req := Detect(dir)
	if req.Node != "20.9.0" || req.NodeSource != "package.json" {
		t.Fatalf("got %+v, want engines 20.9.0", req)
	}
}

func TestDetectToolVersions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".tool-versions", "# pinned\nnodejs 18.20.1\npython 3.11.4\ngolang 1.22.3\n")
	req := Detect(dir)
	if req.Node != "18.20.1" || req.Python != "3.11.4" || req.Go != "1.22.3" || !req.GoExact {
		t.Fatalf("got %+v", req)
	}
}

func TestDetectLTSAliasIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".nvmrc", "lts/iron\n")
	if req := Detect(dir); req.Node != "" {
		t.Fatalf("lts alias should be ignored, got %q", req.Node)
	}
}

func newTestResolver(home string) *Resolver {
	r := New()
	r.Home = home
	r.Environ = func() []string { return []string{"PATH=/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin"} }
	r.LookPath = func(string) (string, error) { return "/usr/bin/fake", nil }
	r.Version = func(string, ...string) string { return "" }
	return r
}

func TestOverlayGoToolchainExact(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n\ngo 1.23\n\ntoolchain go1.23.4\n")
	ov := newTestResolver(t.TempDir()).Overlay(dir)
	if got := envValue(ov.Env, "GOTOOLCHAIN"); got != "go1.23.4+auto" {
		t.Fatalf("GOTOOLCHAIN = %q", got)
	}
}

func TestOverlayGoAutoForMinimum(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n\ngo 1.22\n")
	ov := newTestResolver(t.TempDir()).Overlay(dir)
	if got := envValue(ov.Env, "GOTOOLCHAIN"); got != "auto" {
		t.Fatalf("GOTOOLCHAIN = %q", got)
	}
}

func TestOverlayPicksManagedNodeInstall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".nvmrc", "18\n")

	home := t.TempDir()
	for _, v := range []string{"v18.19.0", "v18.20.1", "v22.1.0"} {
		bin := filepath.Join(home, ".nvm/versions/node", v, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, bin, "node", "#!/bin/sh\n")
	}

	ov := newTestResolver(home).Overlay(dir)
	if len(ov.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", ov.Warnings)
	}
	path := envValue(ov.Env, "PATH")
	want := filepath.Join(home, ".nvm/versions/node/v18.20.1/bin")
	if !strings.HasPrefix(path, want+string(os.PathListSeparator)) {
		t.Fatalf("PATH %q does not start with %q", path, want)
	}
}

func TestOverlayWarnsOnHostMismatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".nvmrc", "18\n")

	r := newTestResolver(t.TempDir())
	r.Version = func(string, ...string) string { return "v22.1.0" }
	ov := r.Overlay(dir)
	if len(ov.Warnings) != 1 || !strings.Contains(ov.Warnings[0], "node 18") {
		t.Fatalf("want one mismatch warning, got %v", ov.Warnings)
	}
}

func TestOverlayNoWarningWhenHostMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".nvmrc", "22\n")

	r := newTestResolver(t.TempDir())
	r.Version = func(string, ...string) string { return "v22.1.0" }
	ov := r.Overlay(dir)
	if len(ov.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", ov.Warnings)
	}
}

func TestOverlayEnsuresHomebrewPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	r := newTestResolver(t.TempDir())
	r.Environ = func() []string { return []string{"PATH=/usr/bin:/bin"} }
	ov := r.Overlay(dir)
	path := envValue(ov.Env, "PATH")
	if !strings.Contains(path, "/opt/homebrew/bin") || !strings.Contains(path, "/usr/local/bin") {
		t.Fatalf("PATH %q missing install locations", path)
	}
}

func TestOverlayEmptyForUndeclaredRepo(t *testing.T) {
	ov := newTestResolver(t.TempDir()).Overlay(t.TempDir())
	if len(ov.Env) != 0 || len(ov.Warnings) != 0 {
		t.Fatalf("expected empty overlay, got %+v", ov)
	}
}
