package repofacts

import (
	"path/filepath"
	"sort"
	"strings"
)

// marker is one "this file existing means this service is wired" rule. The
// table is deliberately file-based: a config file committed to the repo is a
// fact, whereas a service name mentioned in a README is a rumour.
type marker struct {
	file     string // exact basename, matched anywhere in the tree
	name     string
	category string
	detail   string
}

var fileMarkers = []marker{
	// hosting — the category that decides what "shipping" means for a repo
	{file: "vercel.json", name: "Vercel", category: "hosting", detail: "Vercel project config"},
	{file: "netlify.toml", name: "Netlify", category: "hosting"},
	{file: "fly.toml", name: "Fly.io", category: "hosting"},
	{file: "render.yaml", name: "Render", category: "hosting"},
	{file: "Procfile", name: "Heroku-style buildpack host", category: "hosting"},
	{file: "amplify.yml", name: "AWS Amplify", category: "hosting"},
	{file: "wrangler.toml", name: "Cloudflare Workers/Pages", category: "hosting"},
	{file: "firebase.json", name: "Firebase", category: "hosting", detail: "Firebase Hosting/Functions"},
	{file: "static.json", name: "static host", category: "hosting"},

	// infra
	{file: "Dockerfile", name: "Docker", category: "infra", detail: "container image build"},
	{file: "docker-compose.yml", name: "Docker Compose", category: "infra"},
	{file: "docker-compose.yaml", name: "Docker Compose", category: "infra"},
	{file: "skaffold.yaml", name: "Skaffold", category: "infra"},
	{file: "Chart.yaml", name: "Helm", category: "infra"},
	{file: "cloudbuild.yaml", name: "Google Cloud Build", category: "infra"},
	{file: "serverless.yml", name: "Serverless Framework", category: "infra"},

	// data / auth
	{file: "schema.prisma", name: "Prisma", category: "data"},
	{file: "config.toml", name: "Supabase", category: "data", detail: "supabase/config.toml"},
	{file: "google-services.json", name: "Firebase (Android)", category: "auth"},
	{file: "GoogleService-Info.plist", name: "Firebase (iOS)", category: "auth"},

	// observability
	{file: ".sentryclirc", name: "Sentry", category: "observability"},
	{file: "sentry.properties", name: "Sentry", category: "observability"},
	{file: "datadog.yaml", name: "Datadog", category: "observability"},

	// mobile release
	{file: "Fastfile", name: "Fastlane", category: "mobile", detail: "app store release automation"},
	{file: "eas.json", name: "Expo EAS", category: "mobile"},
	{file: "Appfile", name: "Fastlane", category: "mobile"},
}

// depMarkers map a dependency already parsed out of a manifest to a service.
// They cover the integrations that ship as a library rather than a config file.
var depMarkers = map[string]marker{
	"stripe":                {name: "Stripe", category: "payments"},
	"firebase":              {name: "Firebase", category: "auth"},
	"@supabase/supabase-js": {name: "Supabase", category: "data"},
	"@sentry/react":         {name: "Sentry", category: "observability"},
	"@sentry/node":          {name: "Sentry", category: "observability"},
	"prisma":                {name: "Prisma", category: "data"},
	"drizzle-orm":           {name: "Drizzle", category: "data"},
}

func collectPlatforms(root string, t *treeScan, f *Facts) {
	seen := map[string]bool{}
	add := func(in Integration) {
		key := in.Name + "|" + in.Category
		if seen[key] {
			return
		}
		seen[key] = true
		f.Integrations = append(f.Integrations, in)
	}

	for _, m := range fileMarkers {
		for _, rel := range t.find(m.file) {
			// supabase/config.toml is the only config.toml worth claiming —
			// the name is too common to trust on its own.
			if m.file == "config.toml" && !strings.Contains(rel, "supabase") {
				continue
			}
			if depth(rel) > 4 {
				continue
			}
			add(Integration{Name: m.name, Category: m.category, Detail: m.detail, Evidence: rel})
			break
		}
	}

	// A .vercel/project.json is written by `vercel link`; it proves the repo is
	// attached to a Vercel project even when vercel.json was never created.
	for _, rel := range t.find("project.json") {
		if strings.Contains(rel, ".vercel/") {
			add(Integration{Name: "Vercel", Category: "hosting", Detail: "linked Vercel project", Evidence: rel})
		}
	}

	// Kubernetes: recognise it from a manifest that actually declares a
	// workload, not from a directory called "k8s".
	for _, rel := range t.files {
		if depth(rel) > 5 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		raw, err := readCapped(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		body := string(raw)
		if strings.Contains(body, "apiVersion: apps/v1") && strings.Contains(body, "kind: Deployment") {
			add(Integration{Name: "Kubernetes", Category: "infra", Detail: "workload manifests", Evidence: rel})
			break
		}
	}
	if tf := t.findSuffix(".tf"); len(tf) > 0 {
		add(Integration{Name: "Terraform", Category: "infra", Evidence: tf[0]})
	}

	for _, m := range f.Manifests {
		for _, dep := range m.Deps {
			name := dep
			if i := strings.IndexByte(name, '@'); i > 0 {
				name = name[:i]
			}
			if mk, ok := depMarkers[name]; ok {
				add(Integration{Name: mk.name, Category: mk.category, Detail: "declared in " + filepath.Base(m.Path), Evidence: m.Path})
			}
		}
	}

	sort.SliceStable(f.Integrations, func(i, j int) bool {
		if f.Integrations[i].Category != f.Integrations[j].Category {
			return f.Integrations[i].Category < f.Integrations[j].Category
		}
		return f.Integrations[i].Name < f.Integrations[j].Name
	})
}

// testConfigs maps a test-runner config basename to the framework it proves.
var testConfigs = []struct {
	prefix    string
	framework string
	command   string
}{
	{prefix: "vitest.config", framework: "Vitest", command: "vitest run"},
	{prefix: "jest.config", framework: "Jest", command: "jest"},
	{prefix: "playwright.config", framework: "Playwright", command: "playwright test"},
	{prefix: "cypress.config", framework: "Cypress", command: "cypress run"},
	{prefix: "karma.conf", framework: "Karma"},
	{prefix: "pytest.ini", framework: "pytest", command: "pytest"},
	{prefix: "conftest.py", framework: "pytest", command: "pytest"},
	{prefix: "phpunit.xml", framework: "PHPUnit"},
}

func collectTestAreas(_ string, t *treeScan, f *Facts) {
	seen := map[string]bool{}
	add := func(a TestArea) {
		key := a.Area + "|" + a.Framework
		if seen[key] {
			return
		}
		seen[key] = true
		f.TestAreas = append(f.TestAreas, a)
	}

	for _, rel := range t.files {
		base := filepath.Base(rel)
		for _, tc := range testConfigs {
			if strings.HasPrefix(base, tc.prefix) {
				add(TestArea{Area: dirOf(rel), Framework: tc.framework, Command: tc.command, Evidence: rel})
			}
		}
		switch {
		case strings.HasSuffix(rel, "_test.go"):
			add(TestArea{Area: goTestArea(rel), Framework: "go test", Command: "go test ./...", Evidence: rel})
		case strings.HasSuffix(rel, "Tests.swift"), strings.HasSuffix(rel, "Test.swift"):
			add(TestArea{Area: dirOf(rel), Framework: "XCTest", Evidence: rel})
		case strings.HasSuffix(rel, "_test.dart"):
			add(TestArea{Area: dirOf(rel), Framework: "flutter test", Command: "flutter test", Evidence: rel})
		}
	}
	sort.SliceStable(f.TestAreas, func(i, j int) bool { return f.TestAreas[i].Area < f.TestAreas[j].Area })
	if len(f.TestAreas) > 12 {
		f.TestAreas = f.TestAreas[:12]
	}
}

// goTestArea reports the module-ish area of a Go test rather than its exact
// package directory: `go test ./...` is run from the module root, so naming
// the leaf package would tell an agent to cd somewhere pointless.
func goTestArea(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) > 2 && parts[0] == "apps" {
		return parts[0] + "/" + parts[1]
	}
	if len(parts) > 1 {
		return parts[0]
	}
	return ""
}
