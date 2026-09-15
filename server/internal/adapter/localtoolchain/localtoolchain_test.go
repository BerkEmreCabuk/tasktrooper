package localtoolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Three rules, each of which fails silently if got wrong:
//
//  1. A constraint is not a pin. ">=3.11" handed to a session as PYTHON_VERSION
//     looks like a pin and selects nothing.
//  2. Absence is absence. A checkout that declares nothing gets no "system" or
//     "latest" default passed on as though the repository had asked for it.
//  3. Env is passable as it stands: every name in it is one the executor's
//     allowlist accepts.

func checkoutWith(t *testing.T, files map[string]string) (*Detector, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "repos", "acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the checkout: %v", err)
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return New(root), dir
}

func detect(t *testing.T, d *Detector, dir string) port.Toolchain {
	t.Helper()
	got, err := d.Detect(context.Background(), dir)
	if err != nil {
		t.Fatalf("Detect(%s): %v", dir, err)
	}
	return got
}

func pinFor(got port.Toolchain, language string) (port.ToolchainPin, bool) {
	for _, p := range got.Pins {
		if p.Language == language {
			return p, true
		}
	}
	return port.ToolchainPin{}, false
}

func TestEachKindOfPinFileBecomesItsEnvironmentName(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		".nvmrc":              "v20.11.0\n",
		".python-version":     "3.12.1\n",
		".ruby-version":       "3.2.2\n",
		".java-version":       "17.0.9\n",
		"go.mod":              "module example.com/x\n\ngo 1.24.0\n",
		"rust-toolchain.toml": "[toolchain]\nchannel = \"1.79.0\"\ncomponents = [\"clippy\"]\n",
		".fvmrc":              `{"flutter":"3.19.0"}`,
	})

	got := detect(t, d, dir)
	want := map[string]struct{ version, envName, envValue string }{
		"node":    {"20.11.0", "NODE_VERSION", "20.11.0"},
		"python":  {"3.12.1", "PYTHON_VERSION", "3.12.1"},
		"ruby":    {"3.2.2", "RUBY_VERSION", "3.2.2"},
		"java":    {"17.0.9", "JAVA_VERSION", "17.0.9"},
		"go":      {"1.24.0", "GOTOOLCHAIN", "go1.24.0"},
		"rust":    {"1.79.0", "RUSTUP_TOOLCHAIN", "1.79.0"},
		"flutter": {"3.19.0", "FLUTTER_VERSION", "3.19.0"},
	}
	for language, expected := range want {
		pin, found := pinFor(got, language)
		if !found {
			t.Fatalf("%s was not detected; pins were %+v", language, got.Pins)
		}
		if pin.Version != expected.version || !pin.Exact {
			t.Fatalf("%s pin = %+v, want exact %q", language, pin, expected.version)
		}
		if got.Env[expected.envName] != expected.envValue {
			t.Fatalf("env[%s] = %q, want %q", expected.envName, got.Env[expected.envName], expected.envValue)
		}
	}
	if len(got.Env) != len(want) {
		t.Fatalf("env = %v, want exactly one entry per language", got.Env)
	}
}

func TestGoVersionFileAndSdkmanAndGemfileAreRead(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		".go-version": "1.23.4\n",
		"Gemfile":     "source 'https://rubygems.org'\nruby \"3.2.2\"\n\ngem 'rails'\n",
		".sdkmanrc":   "# sdkman\njava=17.0.9-tem\nmaven=3.9.6\n",
	})
	got := detect(t, d, dir)
	if got.Env["GOTOOLCHAIN"] != "go1.23.4" {
		t.Fatalf("env = %v, want GOTOOLCHAIN from .go-version", got.Env)
	}
	if got.Env["RUBY_VERSION"] != "3.2.2" {
		t.Fatalf("env = %v, want RUBY_VERSION from the Gemfile", got.Env)
	}
	if got.Env["JAVA_VERSION"] != "17.0.9-tem" {
		t.Fatalf("env = %v, want the vendor suffix kept — it is part of the version sdkman resolves", got.Env)
	}
}

// GOTOOLCHAIN takes a toolchain NAME and go.mod states a language VERSION;
// `GOTOOLCHAIN=1.24.0` selects nothing.
func TestGoIsTranslatedIntoAToolchainNameAndTheToolchainLineWins(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		"go.mod":      "module example.com/x\n\ngo 1.24\n\ntoolchain go1.24.3\n",
		".go-version": "1.22.0\n",
	})
	got := detect(t, d, dir)
	if pin, _ := pinFor(got, "go"); pin.Version != "go1.24.3" {
		t.Fatalf("first go pin = %+v, want the go.mod toolchain line", pin)
	}
	if got.Env["GOTOOLCHAIN"] != "go1.24.3" {
		t.Fatalf("env = %v, want the toolchain line verbatim", got.Env)
	}

	d, dir = checkoutWith(t, map[string]string{"go.mod": "module example.com/x\n\ngo 1.24\n"})
	got = detect(t, d, dir)
	if pin, _ := pinFor(got, "go"); pin.Version != "1.24" {
		t.Fatalf("version = %q, want what go.mod actually says", pin.Version)
	}
	if got.Env["GOTOOLCHAIN"] != "go1.24.0" {
		t.Fatalf("env = %v, want go1.24.0 — GOTOOLCHAIN has no two-component form", got.Env)
	}
}

func TestAConstraintIsNotAPin(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		"package.json":   `{"name":"x","engines":{"node":">=20 <21"}}`,
		"pyproject.toml": "[project]\nname = \"x\"\nrequires-python = \">=3.11\"\n",
		"pubspec.yaml":   "name: x\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n  flutter: \">=3.19.0\"\n",
		".nvmrc":         "lts/hydrogen\n",
	})
	got := detect(t, d, dir)
	if len(got.Pins) == 0 {
		t.Fatal("nothing was detected from files that all state a version")
	}
	for _, pin := range got.Pins {
		if pin.Exact {
			t.Fatalf("%+v was reported as an exact pin; it is a constraint", pin)
		}
	}
	if len(got.Env) != 0 {
		t.Fatalf("env = %v, want empty: not one of these files names a single runtime", got.Env)
	}
}

func TestACheckoutThatPinsNothingReportsNothing(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		"README.md":    "# acme\n",
		"main.py":      "print('hello')\n",
		"index.js":     "console.log(1)\n",
		"package.json": `{"name":"x","version":"1.0.0"}`,
	})
	got := detect(t, d, dir)
	if len(got.Pins) != 0 || len(got.Env) != 0 {
		t.Fatalf("got %+v, want nothing — file extensions say somebody wrote Python, not which Python", got)
	}
}

func TestTwoFilesPinningOneLanguageBothAppearInPrecedenceOrder(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		".tool-versions": "nodejs 20.11.0\n",
		".nvmrc":         "18.19.0\n",
		"package.json":   `{"engines":{"node":">=18"}}`,
	})
	got := detect(t, d, dir)
	var sources []string
	for _, pin := range got.Pins {
		if pin.Language == "node" {
			sources = append(sources, pin.Source)
		}
	}
	if strings.Join(sources, ",") != ".tool-versions,.nvmrc,package.json" {
		t.Fatalf("node sources = %v, want the version manager's own file first", sources)
	}
	if got.Env["NODE_VERSION"] != "20.11.0" {
		t.Fatalf("env[NODE_VERSION] = %q, want the .tool-versions answer", got.Env["NODE_VERSION"])
	}
}

func TestToolVersionsAndMiseAreReadWithTheirOwnConventions(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		".tool-versions": "# managed by asdf\nnodejs 20.11.0 18.19.0\ngolang 1.24.0\nterraform 1.7.5\n",
	})
	got := detect(t, d, dir)
	if pin, _ := pinFor(got, "node"); pin.Version != "20.11.0" {
		t.Fatalf("node = %q, want the first version on the line", pin.Version)
	}
	if got.Env["GOTOOLCHAIN"] != "go1.24.0" {
		t.Fatalf("env = %v, want golang normalised to a toolchain name", got.Env)
	}
	if _, found := pinFor(got, "terraform"); !found {
		t.Fatalf("terraform was dropped; the file declares it: %+v", got.Pins)
	}
	for name := range got.Env {
		if strings.Contains(strings.ToLower(name), "terraform") {
			t.Fatalf("terraform reached the environment as %s; nothing on the allowlist names it", name)
		}
	}

	d, dir = checkoutWith(t, map[string]string{
		"mise.toml": "[env]\nnode = \"999\"\n\n[tools]\nnode = [\"20.11.0\", \"18\"]\npython = \"3.12.1\"\n",
	})
	got = detect(t, d, dir)
	if got.Env["NODE_VERSION"] != "20.11.0" || got.Env["PYTHON_VERSION"] != "3.12.1" {
		t.Fatalf("env = %v, want the [tools] entries and not the [env] one", got.Env)
	}
}

func TestARustChannelIsAnExactPin(t *testing.T) {
	for _, channel := range []string{"stable", "nightly-2024-03-01"} {
		d, dir := checkoutWith(t, map[string]string{"rust-toolchain": channel + "\n"})
		if got := detect(t, d, dir); got.Env["RUSTUP_TOOLCHAIN"] != channel {
			t.Fatalf("env = %v, want RUSTUP_TOOLCHAIN=%s", got.Env, channel)
		}
	}
}

// The detector and the executor hold their halves of one contract in
// different packages; a name added to one and not the other would be dropped
// by the executor on every run.
func TestEveryEmittedNameIsOneTheExecutorAccepts(t *testing.T) {
	for language, name := range envNames {
		if !domain.SessionEnvAllowed(name) {
			t.Fatalf("%s maps onto %s, which the session env allowlist refuses", language, name)
		}
	}
	d, dir := checkoutWith(t, map[string]string{
		".tool-versions": "nodejs 20.11.0\npython 3.12.1\nruby 3.2.2\njava 17.0.9\nrust 1.79.0\nflutter 3.19.0\n",
		"go.mod":         "module x\n\ngo 1.24.0\n",
	})
	got := detect(t, d, dir)
	if len(got.Env) != len(envNames) {
		t.Fatalf("env = %v, want one entry per language", got.Env)
	}
	if _, refused := domain.SessionEnv(got.Env); len(refused) != 0 {
		t.Fatalf("the executor refuses %v from the env this detector produced", refused)
	}
}

func TestDetectRefusesAWorkspaceOutsideTheRoot(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{".nvmrc": "20\n"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, ".nvmrc"), []byte("20.11.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(d.root, "repos", "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{
		"repos/acme",
		"",
		"/etc",
		outside,
		d.root,
		filepath.Join(d.root, "repos", "..", ".."),
		filepath.Join(dir, "..", "..", ".."),
		filepath.Join(d.root, "repos", "never-cloned"),
		link,
		filepath.Join(dir, ".nvmrc"),
	} {
		if _, err := d.Detect(context.Background(), workspace); err == nil {
			t.Fatalf("Detect accepted %q", workspace)
		}
	}
	if (&Detector{}).Available() || New("  ").Available() {
		t.Fatal("a detector with no root must report itself unavailable")
	}
}

func TestASymlinkedPinFileIsNotFollowed(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{"README.md": "x\n"})
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(outside, []byte("20.11.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ".nvmrc")); err != nil {
		t.Fatal(err)
	}
	if got := detect(t, d, dir); len(got.Pins) != 0 {
		t.Fatalf("pins = %+v, want none: the .nvmrc is a link to a file outside the checkout", got.Pins)
	}
}

func TestOnlyTheNamedDirectoryIsRead(t *testing.T) {
	d, dir := checkoutWith(t, map[string]string{
		"web/.nvmrc":     "20.11.0\n",
		"api/go.mod":     "module x\n\ngo 1.24.0\n",
		".tool-versions": "python 3.12.1\n",
	})
	got := detect(t, d, dir)
	if len(got.Pins) != 1 || got.Pins[0].Language != "python" {
		t.Fatalf("pins = %+v, want only the root's own", got.Pins)
	}
	if deeper := detect(t, d, filepath.Join(dir, "web")); len(deeper.Pins) != 1 || deeper.Pins[0].Language != "node" {
		t.Fatalf("pins for web = %+v, want the node pin", deeper.Pins)
	}
}
