package repofacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// maxManifestBytes caps a manifest read. Manifests are small; anything larger
// is a generated blob and parsing it buys nothing.
const maxManifestBytes = 512 * 1024

// frameworkDeps are the npm dependencies worth naming in a profile — the ones
// that decide how the code is written. Everything else is noise at this
// altitude.
var frameworkDeps = map[string]bool{
	"react": true, "next": true, "vue": true, "nuxt": true, "svelte": true,
	"@sveltejs/kit": true, "@angular/core": true, "solid-js": true, "astro": true,
	"vite": true, "webpack": true, "expo": true, "react-native": true,
	"express": true, "fastify": true, "@nestjs/core": true, "hono": true,
	"tailwindcss": true, "typescript": true, "vitest": true, "jest": true,
	"@playwright/test": true, "cypress": true, "electron": true,
	"@tanstack/react-query": true, "redux": true, "zustand": true, "prisma": true,
	"drizzle-orm": true, "@supabase/supabase-js": true, "firebase": true,
	"stripe": true, "@sentry/react": true, "@sentry/node": true,
}

// scriptPurpose maps an npm script name to the profile's command purpose. Only
// scripts that map are reported: a repo's "postinstall" is not a command an
// agent should ever be told to run.
var scriptPurpose = map[string]string{
	"build": "build", "build:prod": "build", "compile": "build",
	"test": "test", "test:unit": "test", "test:ci": "test", "test:e2e": "test",
	"dev": "run", "start": "run", "serve": "run", "preview": "run",
	"lint": "lint", "lint:fix": "lint",
	"typecheck": "typecheck", "type-check": "typecheck", "tsc": "typecheck",
	"migrate": "migrate", "db:migrate": "migrate",
}

func collectManifests(root string, t *treeScan, f *Facts) {
	for _, p := range t.find("package.json") {
		if depth(p) > 3 {
			continue
		}
		readPackageJSON(root, p, t, f)
	}
	for _, p := range t.find("go.mod") {
		readGoMod(root, p, t, f)
	}
	for _, p := range t.find("pyproject.toml") {
		readSimpleManifest(root, p, "python", f)
	}
	for _, p := range t.find("requirements.txt") {
		if depth(p) > 2 {
			continue
		}
		f.Manifests = append(f.Manifests, Manifest{Path: p, Ecosystem: "python", Manager: "pip"})
	}
	for _, p := range t.find("Cargo.toml") {
		readSimpleManifest(root, p, "rust", f)
	}
	for _, p := range t.find("pubspec.yaml") {
		readSimpleManifest(root, p, "dart", f)
	}
	for _, p := range t.find("Package.swift") {
		f.Manifests = append(f.Manifests, Manifest{Path: p, Ecosystem: "swift", Manager: "spm"})
	}
	for _, p := range t.find("Podfile") {
		f.Manifests = append(f.Manifests, Manifest{Path: p, Ecosystem: "swift", Manager: "cocoapods"})
	}
	for _, p := range t.find("Gemfile") {
		f.Manifests = append(f.Manifests, Manifest{Path: p, Ecosystem: "ruby", Manager: "bundler"})
	}
	for _, p := range t.find("pom.xml") {
		f.Manifests = append(f.Manifests, Manifest{Path: p, Ecosystem: "java", Manager: "maven"})
	}
	for _, p := range append(t.find("build.gradle"), t.find("build.gradle.kts")...) {
		if depth(p) > 2 {
			continue
		}
		f.Manifests = append(f.Manifests, Manifest{Path: p, Ecosystem: "java", Manager: "gradle"})
	}
	for _, p := range t.find("composer.json") {
		f.Manifests = append(f.Manifests, Manifest{Path: p, Ecosystem: "php", Manager: "composer"})
	}

	for _, p := range t.find("Makefile") {
		if depth(p) > 2 {
			continue
		}
		readMakefile(root, p, f)
	}
	for _, p := range append(t.find("Taskfile.yml"), t.find("Taskfile.yaml")...) {
		f.Commands = append(f.Commands, Command{Purpose: "build", Area: dirOf(p), Cmd: "task <target>", Source: p})
	}

	sort.SliceStable(f.Manifests, func(i, j int) bool { return f.Manifests[i].Path < f.Manifests[j].Path })
	dedupeCommands(f)
}

func readPackageJSON(root, rel string, t *treeScan, f *Facts) {
	raw, err := readCapped(filepath.Join(root, rel))
	if err != nil {
		f.Warnings = append(f.Warnings, "could not read "+rel)
		return
	}
	var pkg struct {
		Name            string            `json:"name"`
		PackageManager  string            `json:"packageManager"`
		Private         bool              `json:"private"`
		Workspaces      json.RawMessage   `json:"workspaces"`
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Engines         map[string]string `json:"engines"`
	}
	if json.Unmarshal(raw, &pkg) != nil {
		f.Warnings = append(f.Warnings, rel+" is not valid JSON")
		return
	}

	m := Manifest{Path: rel, Ecosystem: "npm", Name: pkg.Name, Manager: packageManagerFor(rel, pkg.PackageManager, t)}
	if v := pkg.Engines["node"]; v != "" {
		m.Version = "node " + v
	}
	for name, ver := range pkg.Dependencies {
		if frameworkDeps[name] {
			m.Deps = append(m.Deps, name+"@"+strings.TrimLeft(ver, "^~"))
		}
	}
	for name, ver := range pkg.DevDependencies {
		if frameworkDeps[name] {
			m.Deps = append(m.Deps, name+"@"+strings.TrimLeft(ver, "^~"))
		}
	}
	sort.Strings(m.Deps)
	m.Workspace = parseWorkspaces(pkg.Workspaces)
	f.Manifests = append(f.Manifests, m)

	area := dirOf(rel)
	runner := runnerFor(m.Manager)
	for name, body := range pkg.Scripts {
		purpose, ok := scriptPurpose[name]
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		f.Commands = append(f.Commands, Command{
			Purpose: purpose,
			Area:    area,
			Cmd:     runner + " " + name,
			Source:  rel,
		})
	}
}

// packageManagerFor resolves the manager from the lockfile sitting next to the
// manifest — the only source that cannot be wrong. A "packageManager" field is
// used as a fallback because it states intent even before the lock exists.
func packageManagerFor(rel, declared string, t *treeScan) string {
	dir := dirOf(rel)
	join := func(name string) string {
		if dir == "" {
			return name
		}
		return dir + "/" + name
	}
	switch {
	case t.has(join("pnpm-lock.yaml")):
		return "pnpm"
	case t.has(join("yarn.lock")):
		return "yarn"
	case t.has(join("bun.lockb")), t.has(join("bun.lock")):
		return "bun"
	case t.has(join("package-lock.json")):
		return "npm"
	}
	if declared != "" {
		if i := strings.IndexByte(declared, '@'); i > 0 {
			return declared[:i]
		}
		return declared
	}
	return "npm"
}

func runnerFor(manager string) string {
	switch manager {
	case "pnpm":
		return "pnpm"
	case "yarn":
		return "yarn"
	case "bun":
		return "bun run"
	default:
		return "npm run"
	}
}

func parseWorkspaces(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var globs []string
	if json.Unmarshal(raw, &globs) == nil {
		return globs
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Packages
	}
	return nil
}

var goModuleRe = regexp.MustCompile(`(?m)^module\s+(\S+)`)
var goVersionRe = regexp.MustCompile(`(?m)^go\s+(\S+)`)

func readGoMod(root, rel string, t *treeScan, f *Facts) {
	raw, err := readCapped(filepath.Join(root, rel))
	if err != nil {
		f.Warnings = append(f.Warnings, "could not read "+rel)
		return
	}
	m := Manifest{Path: rel, Ecosystem: "go", Manager: "go modules"}
	if mm := goModuleRe.FindSubmatch(raw); mm != nil {
		m.Name = string(mm[1])
	}
	if vm := goVersionRe.FindSubmatch(raw); vm != nil {
		m.Version = "go " + string(vm[1])
	}
	// Name the frameworks, not the dependency tree: a profile that lists 80
	// modules is a lockfile, and agents already have grep for that.
	for _, dep := range []string{"gofiber/fiber", "gin-gonic/gin", "labstack/echo", "jackc/pgx", "gorm.io/gorm", "spf13/cobra", "grpc"} {
		if strings.Contains(string(raw), dep) {
			m.Deps = append(m.Deps, dep)
		}
	}
	f.Manifests = append(f.Manifests, m)

	area := dirOf(rel)
	f.Commands = append(f.Commands,
		Command{Purpose: "build", Area: area, Cmd: "go build ./...", Source: rel},
		Command{Purpose: "test", Area: area, Cmd: "go test ./...", Source: rel},
	)
	if t.has(join(area, "main.go")) {
		f.Commands = append(f.Commands, Command{Purpose: "run", Area: area, Cmd: "go run .", Source: join(area, "main.go")})
	}
}

func readSimpleManifest(root, rel, ecosystem string, f *Facts) {
	raw, err := readCapped(filepath.Join(root, rel))
	if err != nil {
		return
	}
	m := Manifest{Path: rel, Ecosystem: ecosystem}
	switch ecosystem {
	case "rust":
		m.Manager = "cargo"
	case "dart":
		m.Manager = "pub"
	case "python":
		m.Manager = pythonManager(string(raw))
	}
	if name := firstMatch(string(raw), `(?m)^name\s*[:=]\s*"?([^"\n]+)"?`); name != "" {
		m.Name = strings.TrimSpace(name)
	}
	f.Manifests = append(f.Manifests, m)

	area := dirOf(rel)
	switch ecosystem {
	case "rust":
		f.Commands = append(f.Commands,
			Command{Purpose: "build", Area: area, Cmd: "cargo build", Source: rel},
			Command{Purpose: "test", Area: area, Cmd: "cargo test", Source: rel})
	case "dart":
		f.Commands = append(f.Commands,
			Command{Purpose: "build", Area: area, Cmd: "flutter build", Source: rel},
			Command{Purpose: "test", Area: area, Cmd: "flutter test", Source: rel})
	}
}

func pythonManager(body string) string {
	switch {
	case strings.Contains(body, "[tool.poetry]"):
		return "poetry"
	case strings.Contains(body, "[tool.uv]"):
		return "uv"
	case strings.Contains(body, "[tool.hatch"):
		return "hatch"
	}
	return "pip"
}

// makeTargetRe matches a Makefile target line, excluding pattern rules and
// variable assignments.
var makeTargetRe = regexp.MustCompile(`(?m)^([a-zA-Z][a-zA-Z0-9_./-]*):(?:[^=]|$)`)

func readMakefile(root, rel string, f *Facts) {
	raw, err := readCapped(filepath.Join(root, rel))
	if err != nil {
		return
	}
	area := dirOf(rel)
	seen := map[string]bool{}
	for _, m := range makeTargetRe.FindAllStringSubmatch(string(raw), -1) {
		target := m[1]
		if seen[target] {
			continue
		}
		seen[target] = true
		purpose := ""
		switch {
		case target == "build" || strings.HasPrefix(target, "build-"):
			purpose = "build"
		case target == "test" || strings.HasPrefix(target, "test-"):
			purpose = "test"
		case target == "run" || target == "dev" || target == "up":
			purpose = "run"
		case target == "lint" || target == "vet":
			purpose = "lint"
		case strings.Contains(target, "migrate"):
			purpose = "migrate"
		}
		if purpose == "" {
			continue
		}
		f.Commands = append(f.Commands, Command{Purpose: purpose, Area: area, Cmd: "make " + target, Source: rel})
	}
}

// dedupeCommands keeps one command per (purpose, area, cmd). A monorepo whose
// root and app both declare `npm run build` should say so once.
func dedupeCommands(f *Facts) {
	seen := map[string]bool{}
	out := f.Commands[:0]
	for _, c := range f.Commands {
		key := c.Purpose + "|" + c.Area + "|" + c.Cmd
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	f.Commands = out
	sort.SliceStable(f.Commands, func(i, j int) bool {
		if f.Commands[i].Area != f.Commands[j].Area {
			return f.Commands[i].Area < f.Commands[j].Area
		}
		return f.Commands[i].Purpose < f.Commands[j].Purpose
	})
}

func readCapped(path string) ([]byte, error) {
	fh, err := os.Open(path) //nolint:gosec // path is inside the repository working copy
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	buf := make([]byte, maxManifestBytes)
	n, err := fh.Read(buf)
	if n == 0 && err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func firstMatch(body, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func depth(rel string) int { return strings.Count(rel, "/") + 1 }

func dirOf(rel string) string {
	dir := filepath.Dir(rel)
	if dir == "." {
		return ""
	}
	return filepath.ToSlash(dir)
}

func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}
