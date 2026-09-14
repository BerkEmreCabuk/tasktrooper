package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DetectRepoKind guesses a repository's kind from what is on disk, the same way
// board/commands.go guesses build and test commands: cheap marker files, no
// network, no LLM. It exists because kind used to be set nowhere at import —
// every repo landed as "backend", which silently mis-picks pipeline keywords,
// deploy templates and the role that owns deploy work.
//
// It is a guess, not a verdict: the import dialog shows the result and the
// human can change it, and Deploy settings can change it later.
//
// Rules are ordered, most specific first:
//
//  1. mobile   — Flutter/iOS/Android app markers (an Android app is recognised
//     by its manifest, not by Gradle: half the JVM backends in
//     the world build with Gradle)
//  2. monorepo — several marker-bearing projects under apps/ or packages/, or
//     a Go module and a Node package living at different levels
//  3. frontend — a root package.json whose dependencies name a web framework
//  4. backend  — everything else: the server-side markers (go.mod, pom.xml,
//     build.gradle, requirements.txt, pyproject.toml) and the fallback answer
//     agree here, which is also the repositories.kind column default
func DetectRepoKind(rootPath string) string {
	root := strings.TrimSpace(rootPath)
	if root == "" {
		return domain.RepoKindBackend
	}
	if looksMobile(root) {
		return domain.RepoKindMobile
	}
	if looksMonorepo(root) {
		return domain.RepoKindMonorepo
	}
	return detectLeafKind(root)
}

// detectLeafKind is DetectRepoKind without the mobile/monorepo checks: the
// answer for a directory known (or assumed) to be a single, non-mobile
// project. Reused to classify each of a monorepo's sub-projects with exactly
// the frontend/backend rule that classifies a whole repository.
func detectLeafKind(root string) string {
	if looksFrontend(root) {
		return domain.RepoKindFrontend
	}
	return domain.RepoKindBackend
}

// DetectRepoSubProjects lists a monorepo's sub-projects with a kind for each,
// using the same marker files that produced the monorepo verdict in the first
// place — so a row here is never something the monorepo detection could not
// itself see. Returns nil for a tree that is not a monorepo, so a caller can
// store the result only where "sub_projects" means something.
func DetectRepoSubProjects(rootPath string) []domain.RepoSubProject {
	root := strings.TrimSpace(rootPath)
	if root == "" || !looksMonorepo(root) {
		return nil
	}
	dirs := subProjectDirs(root)
	out := make([]domain.RepoSubProject, 0, len(dirs))
	for _, rel := range dirs {
		abs := root
		if rel != "." {
			abs = filepath.Join(root, filepath.FromSlash(rel))
		}
		kind, platform := classifyDir(abs)
		sub := domain.RepoSubProject{Path: rel, Kind: kind, MobilePlatform: platform}
		if kind == domain.RepoKindMobile {
			sub.DetectedAppIdentity = DetectAppIdentity(abs)
			sub.DetectedBuildTargets = DetectBuildTargets(abs)
		}
		out = append(out, sub)
	}
	return out
}

// classifyDir is DetectRepoKind's mobile-then-leaf rule applied to a single
// directory that is already known (or assumed) to be one project — a
// monorepo's child never gets its own monorepo check, since sub-projects do
// not nest. Shared by DetectRepoSubProjects and DetectDirectoryKind (the
// manual "add a sub-project" folder picker's per-pick classification) so a
// path is never classified two different ways depending on which caller
// found it.
//
// The second result is the mobile platform, and it is "" for everything that
// is not mobile: the field only means anything there.
func classifyDir(abs string) (string, string) {
	if looksMobile(abs) {
		return domain.RepoKindMobile, DetectMobilePlatform(abs)
	}
	return detectLeafKind(abs), ""
}

// DetectDirectoryKind classifies one directory inside root — the manual
// "add a sub-project" folder picker's answer for a folder the human picked
// rather than one DetectRepoSubProjects found on its own. rel is repo-relative
// ("." or "" for the root itself). Returns the kind and, for a mobile one, the
// platform it targets.
func DetectDirectoryKind(root, rel string) (string, string) {
	abs := strings.TrimSpace(root)
	rel = strings.TrimSpace(rel)
	if rel != "" && rel != "." {
		abs = filepath.Join(abs, filepath.FromSlash(rel))
	}
	return classifyDir(abs)
}

// DetectMobilePlatform guesses which platform a mobile project targets, the
// same way DetectRepoKind guesses its kind: marker files, no network, no LLM.
// It answers "" for anything it cannot place, which is the unset value — a
// wrong platform sends a run looking for an Xcode project that is not there,
// so silence is the better failure.
//
// Rules are ordered, most specific first:
//
//  1. cross_platform — a Flutter app, or a tree that ships BOTH platform
//     folders (React Native / Capacitor / Cordova all do)
//  2. ios            — only Apple markers: an Xcode project or workspace, an
//     XcodeGen spec, a SwiftPM manifest, or a bare ios/ folder
//  3. android        — only Android markers: Gradle files, the wrapper, a
//     manifest, or a bare android/ folder
//  4. ""             — neither, or nothing recognisable
//
// A tree bearing both kinds of marker without the two folders is still
// cross_platform: two native app targets in one repository is the same answer
// for every consumer of this field.
func DetectMobilePlatform(dir string) string {
	root := strings.TrimSpace(dir)
	if root == "" {
		return ""
	}
	hasAndroidDir := dirExists(filepath.Join(root, "android"))
	hasIOSDir := dirExists(filepath.Join(root, "ios"))
	if looksFlutterApp(root) || (hasAndroidDir && hasIOSDir) {
		return domain.MobilePlatformCross
	}
	ios := hasIOSDir || hasAppleProject(root)
	android := hasAndroidDir || hasGradleProject(root)
	switch {
	case ios && android:
		return domain.MobilePlatformCross
	case ios:
		return domain.MobilePlatformIOS
	case android:
		return domain.MobilePlatformAndroid
	}
	return ""
}

// webFrameworks are the dependency names that make a package.json a frontend
// rather than a Node service. Matched as a prefix so scoped/sub-packages
// (@vitejs/plugin-react, next-auth, react-dom) count too.
var webFrameworks = []string{"react", "vue", "svelte", "vite", "next"}

// ecosystemMarkers identify a directory as a project of its own; used to count
// sub-projects when deciding whether a tree is a monorepo.
var ecosystemMarkers = []string{
	"go.mod", "package.json", "pubspec.yaml", "pom.xml", "build.gradle",
	"build.gradle.kts", "requirements.txt", "pyproject.toml", "Cargo.toml",
	"Package.swift", "composer.json", "Gemfile",
}

func looksMobile(root string) bool {
	if looksFlutterApp(root) || hasAppleProject(root) || hasAndroidManifest(root) {
		return true
	}
	// React Native / Capacitor / Cordova all ship both platform folders.
	return dirExists(filepath.Join(root, "android")) && dirExists(filepath.Join(root, "ios"))
}

// looksFlutterApp: pubspec.yaml alone is any Dart package, the flutter:
// section is what makes it an app.
func looksFlutterApp(root string) bool {
	body, err := os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	return err == nil && strings.Contains(string(body), "flutter:")
}

// hasAppleProject: an Xcode project or workspace, an XcodeGen spec, or a
// SwiftPM manifest. Deliberately not the bare ios/ folder — that folder is
// half of a cross-platform layout and says nothing on its own about whether
// the tree is a mobile project at all, which is what looksMobile asks.
func hasAppleProject(root string) bool {
	if fileExists(filepath.Join(root, "project.yml")) || fileExists(filepath.Join(root, "Package.swift")) {
		return true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".xcodeproj") || strings.HasSuffix(e.Name(), ".xcworkspace") {
			return true
		}
	}
	return false
}

// hasAndroidManifest is the one Android marker specific enough to identify a
// mobile project on its own: a Gradle build or a wrapper is just as likely to
// be a JVM backend, but nothing except an Android app ships a manifest.
func hasAndroidManifest(root string) bool {
	return fileExists(filepath.Join(root, "AndroidManifest.xml")) ||
		fileExists(filepath.Join(root, "app", "src", "main", "AndroidManifest.xml"))
}

// hasGradleProject is the Android half of the platform question. It takes the
// looser markers too, because it is only consulted once a tree is already
// known to be mobile (DetectMobilePlatform) — there, a Gradle build is Android
// rather than a backend.
func hasGradleProject(root string) bool {
	return hasAndroidManifest(root) ||
		hasAnyMarker(root, "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradlew")
}

func looksMonorepo(root string) bool {
	return len(subProjectDirs(root)) > 1
}

// subProjectDirs returns the repo-relative, slash-separated directories that
// look like projects of their own: any apps/* or packages/* child bearing an
// ecosystem marker, plus — when they live at different paths — the directory
// holding go.mod and the one holding package.json (the classic backend + web
// split, server at the root with web/, or both under apps/). "." denotes the
// repository root itself. Deduplicated, not sorted in any particular order
// beyond the scan order of the two sources.
func subProjectDirs(root string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(rel string) {
		if rel == "" {
			rel = "."
		}
		if !seen[rel] {
			seen[rel] = true
			dirs = append(dirs, rel)
		}
	}
	// Any top-level directory that is itself an ecosystem project (a marker
	// file one level down) is a sub-project candidate regardless of naming
	// convention — a monorepo does not have to call its folders "apps" or
	// "packages" to be one; plenty just have "backend/" and "frontend/" at
	// the root.
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if !e.IsDir() || skipDir(e.Name()) {
				continue
			}
			if hasAnyMarker(filepath.Join(root, e.Name()), ecosystemMarkers...) {
				add(e.Name())
			}
		}
	}
	for _, workspace := range []string{"apps", "packages"} {
		entries, err := os.ReadDir(filepath.Join(root, workspace))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if hasAnyMarker(filepath.Join(root, workspace, e.Name()), ecosystemMarkers...) {
				add(workspace + "/" + e.Name())
			}
		}
	}
	goDir, goFound := findMarkerDir(root, "go.mod")
	nodeDir, nodeFound := findMarkerDir(root, "package.json")
	if goFound && nodeFound && goDir != nodeDir {
		add(relDir(root, goDir))
		add(relDir(root, nodeDir))
	}
	return dirs
}

// relDir converts findMarkerDir's absolute result back to the repo-relative,
// slash-separated form DetectRepoSubProjects and subProjectDirs' callers use.
func relDir(root, dir string) string {
	if dir == root {
		return "."
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "."
	}
	return filepath.ToSlash(rel)
}

func looksFrontend(root string) bool {
	body, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return false
	}
	for _, deps := range []map[string]json.RawMessage{pkg.Dependencies, pkg.DevDependencies} {
		for name := range deps {
			trimmed := strings.TrimPrefix(name, "@")
			for _, fw := range webFrameworks {
				if strings.HasPrefix(trimmed, fw) || strings.Contains(trimmed, "/"+fw) {
					return true
				}
			}
		}
	}
	return false
}

// findMarkerDir looks for marker at the root and one level down (the depth a
// monorepo puts its projects at), returning the directory that holds it.
// Scanning deeper would find vendored copies and answer with noise.
func findMarkerDir(root, marker string) (string, bool) {
	if fileExists(filepath.Join(root, marker)) {
		return root, true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() || skipDir(e.Name()) {
			continue
		}
		first := filepath.Join(root, e.Name())
		if fileExists(filepath.Join(first, marker)) {
			return first, true
		}
		nested, err := os.ReadDir(first)
		if err != nil {
			continue
		}
		for _, n := range nested {
			if !n.IsDir() || skipDir(n.Name()) {
				continue
			}
			second := filepath.Join(first, n.Name())
			if fileExists(filepath.Join(second, marker)) {
				return second, true
			}
		}
	}
	return "", false
}

// ListChildDirectories returns the immediate subdirectory names under abs,
// alphabetically sorted, skipping the same noise directories the monorepo
// scan itself skips (.git, node_modules, vendor, dist, build, .venv, target,
// any dotdir). Backs the "pick a folder" UI for manually adding a sub-project
// that DetectRepoSubProjects didn't find on its own.
func ListChildDirectories(abs string) ([]string, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || skipDir(e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".venv", "target":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func hasAnyMarker(dir string, markers ...string) bool {
	for _, m := range markers {
		if fileExists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
