package components

import (
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/dotnet"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type dotnetEcosystem struct{}

func (dotnetEcosystem) Name() string { return "dotnet" }

// maxDotnetProjects bounds how many project files one solution reads. A
// solution with 120 projects is real, but its stack and commands are decided
// long before the last one.
const maxDotnetProjects = 40

// dotnetTestMarkers make a project a test project even when it does not set
// <IsTestProject>: any of these references means `dotnet test` has something
// to run in it.
var dotnetTestMarkers = []string{"Microsoft.NET.Test.Sdk", "xunit", "NUnit", "MSTest", "TUnit"}

var (
	packageRefRe      = regexp.MustCompile(`<PackageReference[^>]*\sInclude="([^"]+)"(?:[^>]*\sVersion="([^"]+)")?`)
	csprojSdkRe       = regexp.MustCompile(`<Project[^>]*\sSdk="([^"]+)"`)
	csprojFrameworkRe = regexp.MustCompile(`<TargetFrameworks?>([^<]+)</TargetFrameworks?>`)
	csprojTestFlagRe  = regexp.MustCompile(`<IsTestProject>\s*true\s*</IsTestProject>`)
)

// Roots are solution directories, plus project directories no solution
// covers. A solution is the .NET way of saying "these projects are one
// product", so Api/ and Worker/ under Shop.sln are one component, not a
// monorepo of two. Unity projects are skipped: the editor regenerates their
// .csproj/.sln files and they are not a dotnet CLI build.
func (dotnetEcosystem) Roots(tree *inventory.Tree) []string {
	unity := unityRoots(tree)
	usable := func(d string) bool { return !ignoredDir(d) && !underAny(d, unity) }

	solutionDirs := map[string]bool{}
	for _, f := range tree.Files {
		if dotnet.IsSolutionFile(baseName(f)) && usable(inventory.Dir(f)) {
			solutionDirs[inventory.Dir(f)] = true
		}
	}
	dirs := map[string]bool{}
	for d := range solutionDirs {
		dirs[d] = true
	}
	for _, f := range tree.Files {
		d := inventory.Dir(f)
		if !dotnet.IsProjectFile(baseName(f)) || !usable(d) || underAny(d, solutionDirs) {
			continue
		}
		dirs[d] = true
	}
	return sortedSet(dirs)
}

// Read describes a solution (named after it, with every project's packages
// folded in) or a lone project. Path is what `dotnet build` is pointed at.
func (dotnetEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	var solutions, projects []string
	for _, f := range tree.Under(dir) {
		if inventory.Dir(f) != dir {
			continue
		}
		switch {
		case dotnet.IsSolutionFile(baseName(f)):
			solutions = append(solutions, f)
		case dotnet.IsProjectFile(baseName(f)):
			projects = append(projects, f)
		}
	}
	sort.Strings(solutions)
	sort.Strings(projects)

	var path string
	var members []dotnetProject
	switch {
	case len(solutions) > 0:
		if picked, ok := dotnet.PickSolution(solutions); ok {
			path = picked
		} else {
			path = solutions[0]
		}
		members = dotnetProjectsUnder(tree, dir)
	case len(projects) > 0:
		path = projects[0]
		members = []dotnetProject{readDotnetProject(tree, path)}
	default:
		return nil
	}

	info := &ManifestInfo{
		Ecosystem:    "dotnet",
		Path:         path,
		Name:         strings.TrimSuffix(baseName(path), pathExt(path)),
		Dependencies: map[string]string{},
	}
	for _, p := range members {
		for name, version := range p.Packages {
			info.Dependencies[name] = version
		}
		if p.SDK != "" && p.SDK != "Microsoft.NET.Sdk" {
			info.Dependencies[p.SDK] = ""
		}
		if info.LanguageVersion == "" && p.Framework != "" && !p.Test {
			info.LanguageVersion = p.Framework
		}
		if p.Runnable {
			info.HasPackageMain = true
		}
	}
	return info
}

type dotnetProject struct {
	Path      string
	SDK       string
	Framework string
	Packages  map[string]string
	Test      bool
	Runnable  bool
	EFCore    bool
}

func dotnetProjectsUnder(tree *inventory.Tree, dir string) []dotnetProject {
	var out []dotnetProject
	for _, f := range tree.Under(dir) {
		if !dotnet.IsProjectFile(baseName(f)) || ignoredDir(inventory.Dir(f)) {
			continue
		}
		out = append(out, readDotnetProject(tree, f))
		if len(out) >= maxDotnetProjects {
			break
		}
	}
	return out
}

func readDotnetProject(tree *inventory.Tree, path string) dotnetProject {
	body := tree.ReadString(path)
	p := dotnetProject{Path: path, Packages: map[string]string{}, Test: csprojTestFlagRe.MatchString(body)}
	if m := csprojSdkRe.FindStringSubmatch(body); m != nil {
		p.SDK = m[1]
	}
	if m := csprojFrameworkRe.FindStringSubmatch(body); m != nil {
		p.Framework = strings.TrimSpace(strings.Split(m[1], ";")[0])
	}
	for _, m := range packageRefRe.FindAllStringSubmatch(body, -1) {
		name := m[1]
		p.Packages[name] = m[2]
		for _, marker := range dotnetTestMarkers {
			if strings.HasPrefix(name, marker) {
				p.Test = true
			}
		}
		if strings.HasPrefix(name, "Microsoft.EntityFrameworkCore") {
			p.EFCore = true
		}
	}
	p.Runnable = !p.Test && (strings.HasPrefix(p.SDK, "Microsoft.NET.Sdk.Web") ||
		strings.HasPrefix(p.SDK, "Microsoft.NET.Sdk.Worker") ||
		strings.Contains(body, "<OutputType>Exe</OutputType>"))
	return p
}

// dotnetCommands points build/test at the solution or lone project, runs the
// single runnable project, and offers an EF Core migration when exactly one
// project owns the DbContext.
func dotnetCommands(tree *inventory.Tree, dir string, info *ManifestInfo) []domain.DetectedCommand {
	target := relativeTo(dir, info.Path)
	ev := domain.SourceEvidence{Path: info.Path}
	out := []domain.DetectedCommand{
		{Purpose: domain.CommandInstall, Command: "dotnet restore " + target, Source: ev},
		{Purpose: domain.CommandBuild, Command: "dotnet build " + target, Source: ev},
		{Purpose: domain.CommandTest, Command: "dotnet test " + target, Source: ev},
	}
	if tree.Has(inventory.Join(dir, ".editorconfig")) {
		out = append(out, domain.DetectedCommand{Purpose: domain.CommandFormat, Command: "dotnet format " + target + " --verify-no-changes", Source: domain.SourceEvidence{Path: inventory.Join(dir, ".editorconfig")}})
	}

	var runnable, ef []dotnetProject
	projects := dotnetProjectsUnder(tree, dir)
	for _, p := range projects {
		if p.Runnable {
			runnable = append(runnable, p)
		}
		if p.EFCore && !p.Test {
			ef = append(ef, p)
		}
	}
	if len(runnable) == 1 {
		out = append(out, domain.DetectedCommand{Purpose: domain.CommandDev, Command: "dotnet run --project " + relativeTo(dir, runnable[0].Path), Source: domain.SourceEvidence{Path: runnable[0].Path}})
	}
	if len(ef) == 1 {
		out = append(out, domain.DetectedCommand{Purpose: domain.CommandMigrate, Command: "dotnet ef database update --project " + relativeTo(dir, ef[0].Path), Source: domain.SourceEvidence{Path: ef[0].Path}})
	}
	return out
}

// unityRoots are the directories that hold a Unity project: the parent of
// ProjectSettings/ProjectVersion.txt, which every Unity version writes.
func unityRoots(tree *inventory.Tree) map[string]bool {
	out := map[string]bool{}
	for _, p := range tree.ByBase("ProjectVersion.txt") {
		settings := inventory.Dir(p)
		if baseName(settings) != "ProjectSettings" {
			continue
		}
		out[inventory.Dir(settings)] = true
	}
	return out
}

func underAny(dir string, roots map[string]bool) bool {
	for r := range roots {
		if r == "." || dir == r || strings.HasPrefix(dir, r+"/") {
			return true
		}
	}
	return false
}

func pathExt(p string) string {
	if i := strings.LastIndexByte(p, '.'); i > strings.LastIndexByte(p, '/') {
		return p[i:]
	}
	return ""
}
