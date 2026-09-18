package repofacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// seed writes a tree of files and returns its root.
func seed(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestCommandsComeFromDeclarations pins the whole point of the package: the
// commands reported are the ones the tree declares, with the file they were
// read from — never a plausible default.
func TestCommandsComeFromDeclarations(t *testing.T) {
	root := seed(t, map[string]string{
		"package.json":   `{"name":"web","packageManager":"pnpm@9.0.0","scripts":{"build":"vite build","test":"vitest run","dev":"vite","postinstall":"patch-package"},"dependencies":{"react":"^19.0.0","vite":"^6.0.0"}}`,
		"pnpm-lock.yaml": "lockfileVersion: '9.0'\n",
		"src/main.tsx":   "export {}\n",
	})
	f := Collect(context.Background(), root)

	var build, test, run string
	for _, c := range f.Commands {
		switch c.Purpose {
		case "build":
			build = c.Cmd
		case "test":
			test = c.Cmd
		case "run":
			run = c.Cmd
		}
		if c.Source == "" {
			t.Errorf("command %q has no source file", c.Cmd)
		}
	}
	if build != "pnpm build" {
		t.Errorf("build command = %q, want the lockfile's package manager", build)
	}
	if test != "pnpm test" {
		t.Errorf("test command = %q", test)
	}
	if run == "" {
		t.Error("dev script must be reported as a run command")
	}
	for _, c := range f.Commands {
		if strings.Contains(c.Cmd, "postinstall") {
			t.Error("only meaningful scripts belong in the profile")
		}
	}
	if len(f.Manifests) != 1 || f.Manifests[0].Manager != "pnpm" {
		t.Fatalf("manifest = %+v, want one npm manifest resolved to pnpm", f.Manifests)
	}
}

// TestDeployFromWorkflow: a workflow whose steps ship is reported with the
// trigger that fires it, and a push trigger is marked automatic.
func TestDeployFromWorkflow(t *testing.T) {
	root := seed(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.26\n",
		".github/workflows/deploy.yml": `name: Deploy
on:
  push:
    branches: [main]
  workflow_dispatch:
jobs:
  deploy-prod:
    steps:
      - run: kubectl apply -f deploy/k8s
`,
		".github/workflows/ci.yml": `name: CI
on: [pull_request]
jobs:
  test:
    steps:
      - run: go test ./...
`,
	})
	f := Collect(context.Background(), root)

	if len(f.Workflows) != 2 {
		t.Fatalf("workflows = %d, want 2", len(f.Workflows))
	}
	var deployWF, ciWF Workflow
	for _, w := range f.Workflows {
		if strings.HasSuffix(w.File, "deploy.yml") {
			deployWF = w
		} else {
			ciWF = w
		}
	}
	if !deployWF.Deploys {
		t.Error("a kubectl apply step must mark the workflow as deploying")
	}
	if ciWF.Deploys {
		t.Error("a test-only workflow must not be marked as deploying")
	}
	if !contains(deployWF.Triggers, "push:main") {
		t.Errorf("triggers = %v, want the branch filter attached", deployWF.Triggers)
	}

	var automatic bool
	for _, d := range f.Deploys {
		if d.Automatic && strings.HasPrefix(d.Trigger, "push:main") {
			automatic = true
			if d.Evidence != ".github/workflows/deploy.yml" {
				t.Errorf("deploy evidence = %q", d.Evidence)
			}
		}
	}
	if !automatic {
		t.Fatalf("landing on main must be reported as an automatic deploy; got %+v", f.Deploys)
	}
	if !strings.Contains(renderGitWorkflow(f), "triggers a") {
		t.Error("the git workflow section must warn that landing on the default branch deploys")
	}
}

// TestHostingIntegrationWithoutWorkflowIsTheDeploy: a Vercel link and no
// deploy workflow means the provider's git integration ships the repo — the
// fact the old free-text profile never stated.
func TestHostingIntegrationWithoutWorkflowIsTheDeploy(t *testing.T) {
	root := seed(t, map[string]string{
		"vercel.json":  `{"framework":"nextjs"}`,
		"package.json": `{"name":"site","scripts":{"build":"next build"},"dependencies":{"next":"15.0.0"}}`,
	})
	f := Collect(context.Background(), root)

	var vercel bool
	for _, in := range f.Integrations {
		if in.Name == "Vercel" && in.Category == "hosting" {
			vercel = true
		}
	}
	if !vercel {
		t.Fatalf("vercel.json must be detected as hosting; integrations = %+v", f.Integrations)
	}
	var prod, preview bool
	for _, d := range f.Deploys {
		if d.Provider != "Vercel" || !d.Automatic {
			continue
		}
		switch d.Environment {
		case "production":
			prod = true
			if !strings.Contains(d.Trigger, "no deploy workflow") {
				t.Errorf("production trigger should say why it is the provider's own hook: %q", d.Trigger)
			}
		case "preview":
			preview = true
		}
	}
	if !prod || !preview {
		t.Fatalf("a linked host with no workflow must yield prod+preview deploys; got %+v", f.Deploys)
	}
}

// TestWorkflowBeatsProviderIntegration: with both a deploy workflow and a
// hosting link, only the workflow is reported — advising the dashboard when
// CI is what ships would send agents to the wrong place.
func TestWorkflowBeatsProviderIntegration(t *testing.T) {
	root := seed(t, map[string]string{
		"vercel.json": `{}`,
		".github/workflows/deploy.yml": `name: Deploy
on:
  push:
    branches: [main]
jobs:
  ship:
    steps:
      - uses: amondnet/vercel-action@v25
`,
	})
	f := Collect(context.Background(), root)
	for _, d := range f.Deploys {
		if d.Provider == "Vercel" {
			t.Fatalf("provider integration must not be reported alongside a deploy workflow: %+v", f.Deploys)
		}
	}
	if len(f.Deploys) == 0 {
		t.Fatal("the deploy workflow must still be reported")
	}
}

// TestMonorepoKindInference: two classifiable children make a monorepo with
// their kinds as sub-projects.
func TestMonorepoKindInference(t *testing.T) {
	files := map[string]string{
		"apps/backend/go.mod":   "module demo\n\ngo 1.26\n",
		"apps/web/package.json": `{"name":"web","dependencies":{"react":"^19.0.0"}}`,
	}
	for i := 0; i < 8; i++ {
		files["apps/backend/internal/svc"+string(rune('a'+i))+".go"] = "package svc\n"
		files["apps/web/src/comp"+string(rune('a'+i))+".tsx"] = "export {}\n"
	}
	f := Collect(context.Background(), seed(t, files))

	if f.Kind != domain.RepoKindMonorepo {
		t.Fatalf("kind = %q, want monorepo (evidence: %v)", f.Kind, f.KindEvidence)
	}
	if !contains(f.SubKinds, domain.RepoKindBackend) || !contains(f.SubKinds, domain.RepoKindFrontend) {
		t.Fatalf("sub kinds = %v, want backend and frontend", f.SubKinds)
	}
	if len(f.KindEvidence) == 0 {
		t.Error("a classification with no evidence is an assertion")
	}
}

// TestSingleKindInference: a mobile app with a few config files is still
// mobile, not frontend.
func TestSingleKindInference(t *testing.T) {
	files := map[string]string{"tsconfig.json": "{}", "Podfile": "platform :ios\n"}
	for i := 0; i < 25; i++ {
		files["App/View"+string(rune('a'+i))+".swift"] = "import SwiftUI\n"
	}
	f := Collect(context.Background(), seed(t, files))
	if f.Kind != domain.RepoKindMobile {
		t.Fatalf("kind = %q, want mobile", f.Kind)
	}
}

// TestDerivedSectionsCarryTheirSources: staleness depends on every derived
// section knowing which files it was read from.
func TestDerivedSectionsCarryTheirSources(t *testing.T) {
	root := seed(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.26\n",
		".github/workflows/ci.yml": `name: CI
on: [push]
jobs:
  test:
    steps:
      - run: go test ./...
`,
	})
	sections := DerivedSections(Collect(context.Background(), root))
	if len(sections) == 0 {
		t.Fatal("no derived sections")
	}
	for _, s := range sections {
		if s.Origin != domain.ProfileOriginDerived {
			t.Errorf("section %q origin = %q", s.Section, s.Origin)
		}
		switch s.Section {
		case domain.ProfileSectionStack, domain.ProfileSectionCommands, domain.ProfileSectionCICD:
			if len(s.SourcePaths) == 0 {
				t.Errorf("section %q has no source paths, so a push can never mark it stale", s.Section)
			}
		}
	}
}

// TestProposalsAreConcrete: the proposals a settings page can apply carry
// values in the shape the settings expect.
func TestProposalsAreConcrete(t *testing.T) {
	root := seed(t, map[string]string{
		"go.mod":   "module demo\n\ngo 1.26\n",
		"main.go":  "package main\n\nfunc main() {}\n",
		"Makefile": "lint:\n\tgolangci-lint run\n",
	})
	proposals := Proposals(Collect(context.Background(), root))
	got := map[string]string{}
	for _, p := range proposals {
		got[p.Field] = string(p.Value)
		if p.Label == "" {
			t.Errorf("proposal %q has no label", p.Field)
		}
	}
	if got[domain.ProposalFieldTestCommand] != `"go test ./..."` {
		t.Errorf("test command proposal = %s", got[domain.ProposalFieldTestCommand])
	}
	if got[domain.ProposalFieldVerifyCommand] != `"make lint"` {
		t.Errorf("verify command proposal = %s", got[domain.ProposalFieldVerifyCommand])
	}
}

// TestMissingWorkingCopyDegrades: a repository whose working copy is gone
// yields warnings and no facts, never a panic and never invented content.
func TestMissingWorkingCopyDegrades(t *testing.T) {
	f := Collect(context.Background(), filepath.Join(t.TempDir(), "nope"))
	if f.HasAny() {
		t.Fatal("a missing working copy must produce no facts")
	}
	if len(f.Warnings) == 0 {
		t.Fatal("a missing working copy must be reported as a warning")
	}
	if PromptFacts(f) != "" {
		t.Fatal("an empty fact block must not be injected")
	}
	if len(DerivedSections(f)) != 0 {
		t.Fatal("no sections may be written from an unreadable tree")
	}
}

func TestDotnetSolutionIsProfiled(t *testing.T) {
	root := seed(t, map[string]string{
		"global.json":   `{"sdk":{"version":"10.0.100","rollForward":"latestFeature"}}`,
		"Shop.sln":      "",
		".editorconfig": "root = true\n",
		"src/Shop.Api/Shop.Api.csproj": `<Project Sdk="Microsoft.NET.Sdk.Web">
  <PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Microsoft.EntityFrameworkCore.Design" Version="10.0.0" />
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
  </ItemGroup>
</Project>`,
		"src/Shop.Api/Program.cs": "var app = WebApplication.Create(args);\n",
		"tests/Shop.Api.Tests/Shop.Api.Tests.csproj": `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup>
  <ItemGroup><PackageReference Include="xunit.v3" Version="3.0.0" /></ItemGroup>
</Project>`,
	})
	f := Collect(context.Background(), root)

	cmds := map[string]string{}
	for _, c := range f.Commands {
		cmds[c.Purpose+"|"+c.Area] = c.Cmd
	}
	if cmds["build|"] != "dotnet build Shop.sln" || cmds["test|"] != "dotnet test Shop.sln" {
		t.Fatalf("solution commands = %+v", f.Commands)
	}
	if cmds["lint|"] != "dotnet format Shop.sln --verify-no-changes" {
		t.Errorf("lint command = %q", cmds["lint|"])
	}
	if cmds["run|src/Shop.Api"] != "dotnet run --project Shop.Api.csproj" {
		t.Errorf("run command = %q", cmds["run|src/Shop.Api"])
	}
	if cmds["migrate|src/Shop.Api"] == "" {
		t.Errorf("an EF Core project should report its migrate command")
	}
	if _, ok := cmds["run|tests/Shop.Api.Tests"]; ok {
		t.Errorf("a test project is not runnable")
	}

	var api *Manifest
	var sdkPinned bool
	for i, m := range f.Manifests {
		if m.Ecosystem != "dotnet" {
			t.Errorf("unexpected ecosystem %q for %s", m.Ecosystem, m.Path)
		}
		if m.Path == "src/Shop.Api/Shop.Api.csproj" {
			api = &f.Manifests[i]
		}
		if m.Path == "global.json" && m.Version == "sdk 10.0.100" {
			sdkPinned = true
		}
	}
	if !sdkPinned {
		t.Errorf("global.json SDK pin not reported: %+v", f.Manifests)
	}
	if api == nil || api.Version != "net10.0" {
		t.Fatalf("api manifest = %+v", api)
	}
	deps := strings.Join(api.Deps, ",")
	if !strings.Contains(deps, "sdk:Microsoft.NET.Sdk.Web") || !strings.Contains(deps, "Microsoft.EntityFrameworkCore.Design@10.0.0") {
		t.Errorf("api deps = %v", api.Deps)
	}
	if strings.Contains(deps, "Newtonsoft") {
		t.Errorf("non-framework package leaked into deps: %v", api.Deps)
	}
	if f.Kind != "backend" {
		t.Errorf("kind = %q, want backend", f.Kind)
	}
}

func TestUnityProjectIsNotProfiledAsDotnet(t *testing.T) {
	root := seed(t, map[string]string{
		"ProjectSettings/ProjectVersion.txt": "m_EditorVersion: 6000.0.30f1\n",
		"Assets/Scripts/Player.cs":           "public class Player {}\n",
		"Game.sln":                           "",
		"Assembly-CSharp.csproj":             `<Project ToolsVersion="4.0"></Project>`,
	})
	f := Collect(context.Background(), root)
	for _, m := range f.Manifests {
		if m.Ecosystem == "dotnet" {
			t.Errorf("unity file profiled as dotnet: %s", m.Path)
		}
	}
	for _, c := range f.Commands {
		if strings.HasPrefix(c.Cmd, "dotnet") {
			t.Errorf("unity project got a dotnet command: %s", c.Cmd)
		}
	}
}
