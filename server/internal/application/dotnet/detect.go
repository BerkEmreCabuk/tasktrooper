// Package dotnet recognises .NET projects from what is on disk. It is a leaf
// package so the repository, board, repofacts and localpreview detectors can
// share one answer: unlike go.mod or package.json, a .NET project has no fixed
// marker filename — the solution and project files are named after the
// product — so an exact-name lookup can never find one.
package dotnet

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectExtensions are the MSBuild project files `dotnet build` accepts.
var ProjectExtensions = []string{".csproj", ".fsproj", ".vbproj"}

// SolutionExtensions are the solution formats: the classic .sln and the XML
// .slnx the .NET 9+ SDK writes by default.
var SolutionExtensions = []string{".sln", ".slnx"}

// FixedMarkers are the SDK-level files whose names do not vary. global.json is
// only a .NET marker when it pins an SDK (see HasGlobalJSONSDK); the
// Directory.*.props/targets files are MSBuild-only.
var FixedMarkers = []string{"Directory.Build.props", "Directory.Packages.props", "Directory.Build.targets"}

// IsProjectFile reports whether name is an MSBuild project file.
func IsProjectFile(name string) bool { return hasAnySuffix(name, ProjectExtensions) }

// IsSolutionFile reports whether name is a solution file.
func IsSolutionFile(name string) bool { return hasAnySuffix(name, SolutionExtensions) }

// IsMarker reports whether a basename marks a .NET project on its own.
func IsMarker(name string) bool {
	if IsProjectFile(name) || IsSolutionFile(name) {
		return true
	}
	for _, m := range FixedMarkers {
		if strings.EqualFold(name, m) {
			return true
		}
	}
	return false
}

// HasProject reports whether dir itself (not its children) holds a .NET
// marker. A Unity project is deliberately not one: Unity regenerates its
// .csproj/.sln files on every editor start, they cannot be built with the
// dotnet CLI, and the build belongs to the Unity editor.
func HasProject(dir string) bool {
	if IsUnityProject(dir) {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && IsMarker(e.Name()) {
			return true
		}
	}
	return HasGlobalJSONSDK(dir)
}

// HasGlobalJSONSDK reports whether dir's global.json pins a .NET SDK. The
// file name is generic enough that other tools could own one, so the "sdk"
// key is what makes it a .NET marker.
func HasGlobalJSONSDK(dir string) bool {
	body, err := os.ReadFile(filepath.Join(dir, "global.json"))
	return err == nil && strings.Contains(string(body), `"sdk"`)
}

// IsUnityProject recognises a Unity project root: Assets/ plus
// ProjectSettings/ProjectVersion.txt, which every Unity version writes.
func IsUnityProject(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "Assets"))
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "ProjectSettings", "ProjectVersion.txt"))
	return err == nil
}

// BuildTarget picks what `dotnet build`/`dotnet test` should be pointed at,
// as a path relative to dir: the single solution at the root, else the single
// project at the root, else the single solution one directory down (the
// src/Product.sln layout). ok is false when there is nothing to build or the
// choice is ambiguous — several solutions, or no solution and several
// projects — because the CLI refuses to pick there, and guessing one would
// silently leave the rest unverified.
func BuildTarget(dir string) (target string, ok bool) {
	solutions, projects := entryFiles(dir)
	switch {
	case len(solutions) > 0:
		return pickSolution(solutions)
	case len(projects) == 1:
		return projects[0], true
	case len(projects) > 1:
		return "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var nested []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub, _ := entryFiles(filepath.Join(dir, e.Name()))
		for _, s := range sub {
			nested = append(nested, filepath.ToSlash(filepath.Join(e.Name(), s)))
		}
	}
	if len(nested) == 0 {
		return "", false
	}
	return pickSolution(nested)
}

// pickSolution resolves the solution list of one directory. A repo shipping
// both formats of the same solution (Foo.sln beside Foo.slnx, common during
// the .slnx migration) builds the .slnx; anything else with more than one
// entry is ambiguous.
func pickSolution(solutions []string) (string, bool) {
	if len(solutions) == 1 {
		return solutions[0], true
	}
	if len(solutions) == 2 {
		a, b := solutions[0], solutions[1]
		stem := func(s string) string { return strings.ToLower(strings.TrimSuffix(s, filepath.Ext(s))) }
		if stem(a) == stem(b) {
			if strings.HasSuffix(strings.ToLower(a), ".slnx") {
				return a, true
			}
			if strings.HasSuffix(strings.ToLower(b), ".slnx") {
				return b, true
			}
		}
	}
	return "", false
}

func entryFiles(dir string) (solutions, projects []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch {
		case IsSolutionFile(e.Name()):
			solutions = append(solutions, e.Name())
		case IsProjectFile(e.Name()):
			projects = append(projects, e.Name())
		}
	}
	sort.Strings(solutions)
	sort.Strings(projects)
	return solutions, projects
}

func hasAnySuffix(name string, suffixes []string) bool {
	lower := strings.ToLower(name)
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}
