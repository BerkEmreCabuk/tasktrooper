package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

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

func detectLeafKind(root string) string {
	if looksFrontend(root) {
		return domain.RepoKindFrontend
	}
	return domain.RepoKindBackend
}

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

func classifyDir(abs string) (string, string) {
	if looksMobile(abs) {
		return domain.RepoKindMobile, DetectMobilePlatform(abs)
	}
	return detectLeafKind(abs), ""
}

func DetectDirectoryKind(root, rel string) (string, string) {
	abs := strings.TrimSpace(root)
	rel = strings.TrimSpace(rel)
	if rel != "" && rel != "." {
		abs = filepath.Join(abs, filepath.FromSlash(rel))
	}
	return classifyDir(abs)
}

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

var webFrameworks = []string{"react", "vue", "svelte", "vite", "next"}

var ecosystemMarkers = []string{
	"go.mod", "package.json", "pubspec.yaml", "pom.xml", "build.gradle",
	"build.gradle.kts", "requirements.txt", "pyproject.toml", "Cargo.toml",
	"Package.swift", "composer.json", "Gemfile",
}

func looksMobile(root string) bool {
	if looksFlutterApp(root) || hasAppleProject(root) || hasAndroidManifest(root) {
		return true
	}

	return dirExists(filepath.Join(root, "android")) && dirExists(filepath.Join(root, "ios"))
}

func looksFlutterApp(root string) bool {
	body, err := os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	return err == nil && strings.Contains(string(body), "flutter:")
}

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

func hasAndroidManifest(root string) bool {
	return fileExists(filepath.Join(root, "AndroidManifest.xml")) ||
		fileExists(filepath.Join(root, "app", "src", "main", "AndroidManifest.xml"))
}

func hasGradleProject(root string) bool {
	return hasAndroidManifest(root) ||
		hasAnyMarker(root, "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradlew")
}

func looksMonorepo(root string) bool {
	return len(subProjectDirs(root)) > 1
}

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
