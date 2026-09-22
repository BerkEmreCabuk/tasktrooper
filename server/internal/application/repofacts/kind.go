package repofacts

import (
	"sort"
	"strings"
)

// The container directories a monorepo keeps its deployables in; a repo with two or more classifiable children under one of these is a monorepo regardless of what its language histogram says.
var appDirs = []string{"apps", "services", "packages"}

// Classifies the repository the way the pipeline settings need it, with the evidence; only a proposal — the settings UI applies it when the field is untouched and offers it when it disagrees with what a human chose.
func inferKind(t *treeScan, f *Facts) {
	children := classifyAppChildren(t, f)
	if len(children) >= 2 {
		f.Kind = "monorepo"
		kinds := make([]string, 0, len(children))
		for dir, kind := range children {
			kinds = append(kinds, kind)
			f.KindEvidence = append(f.KindEvidence, dir+" → "+kind)
		}
		f.SubKinds = dedupeSorted(kinds)
		sort.Strings(f.KindEvidence)
		return
	}

	kind, evidence := classifySingle(t, f, "")
	f.Kind = kind
	if evidence != "" {
		f.KindEvidence = []string{evidence}
	}
}

// Buckets each apps/* (or services/*, packages/*) child by what it contains; children that classify as nothing are dropped rather than defaulted — a packages/tsconfig shared-config folder is not a sub-project.
func classifyAppChildren(t *treeScan, f *Facts) map[string]string {
	out := map[string]string{}
	seen := map[string]bool{}
	for _, rel := range t.files {
		parts := strings.Split(rel, "/")
		if len(parts) < 3 {
			continue
		}
		if !contains(appDirs, parts[0]) {
			continue
		}
		dir := parts[0] + "/" + parts[1]
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if kind, _ := classifySingle(t, f, dir); kind != "" {
			out[dir] = kind
		}
	}
	return out
}

func classifySingle(t *treeScan, f *Facts, prefix string) (kind, evidence string) {
	var goFiles, webFiles, swiftFiles, dartFiles, kotlinFiles, pyFiles int
	scope := prefix
	if scope != "" {
		scope += "/"
	}
	for _, rel := range t.files {
		if scope != "" && !strings.HasPrefix(rel, scope) {
			continue
		}
		switch {
		case strings.HasSuffix(rel, ".go"):
			goFiles++
		case strings.HasSuffix(rel, ".ts"), strings.HasSuffix(rel, ".tsx"),
			strings.HasSuffix(rel, ".jsx"), strings.HasSuffix(rel, ".vue"), strings.HasSuffix(rel, ".svelte"):
			webFiles++
		case strings.HasSuffix(rel, ".swift"):
			swiftFiles++
		case strings.HasSuffix(rel, ".dart"):
			dartFiles++
		case strings.HasSuffix(rel, ".kt"):
			kotlinFiles++
		case strings.HasSuffix(rel, ".py"):
			pyFiles++
		}
	}

	// Mobile wins over everything else it coexists with: an iOS app with a handful of TypeScript config files is still an iOS app, and calling it frontend picks the wrong pipeline keywords (docker instead of xcodebuild).
	switch {
	case swiftFiles > 20 || dartFiles > 20:
		return "mobile", languageEvidence(prefix, "Swift/Dart sources", swiftFiles+dartFiles)
	case kotlinFiles > 20 && hasAndroidMarkers(t, prefix):
		return "mobile", languageEvidence(prefix, "Kotlin sources with Android markers", kotlinFiles)
	}

	if isWorker(t, f, prefix) {
		return "worker", languageEvidence(prefix, "worker/job entrypoints", goFiles+pyFiles)
	}

	backendish := goFiles + pyFiles + kotlinFiles
	switch {
	case backendish > webFiles && backendish > 5:
		return "backend", languageEvidence(prefix, "server-side sources", backendish)
	case webFiles > 5:
		return "frontend", languageEvidence(prefix, "web sources", webFiles)
	case backendish > 0:
		return "backend", languageEvidence(prefix, "server-side sources", backendish)
	}
	return "", ""
}

func languageEvidence(prefix, what string, n int) string {
	where := prefix
	if where == "" {
		where = "repository root"
	}
	return where + ": " + itoa(n) + " " + what
}

func hasAndroidMarkers(t *treeScan, prefix string) bool {
	for _, rel := range append(t.find("AndroidManifest.xml"), t.find("settings.gradle")...) {
		if prefix == "" || strings.HasPrefix(rel, prefix+"/") {
			return true
		}
	}
	return false
}

// Recognises a background processor: an entrypoint under cmd/worker (or similar) with no HTTP server next to it. Getting this wrong is cheap in one direction only — a worker mislabelled backend gets a deploy pipeline it does not need — so the test stays narrow.
func isWorker(t *treeScan, _ *Facts, prefix string) bool {
	for _, rel := range t.files {
		if prefix != "" && !strings.HasPrefix(rel, prefix+"/") {
			continue
		}
		lower := strings.ToLower(rel)
		if strings.HasSuffix(lower, "main.go") || strings.HasSuffix(lower, "main.py") {
			if strings.Contains(lower, "/worker") || strings.Contains(lower, "/consumer") || strings.Contains(lower, "/job") {
				return true
			}
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func dedupeSorted(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
