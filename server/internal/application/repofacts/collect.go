package repofacts

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxWalkFiles bounds the tree walk. A monorepo with a stray build output
// directory can otherwise turn a profile refresh into a filesystem crawl; the
// histogram is a shape, and a shape does not need the tail.
const maxWalkFiles = 40000

// maxLayoutDirs caps how many directories the layout section names. Beyond
// this the profile stops being a brief.
const maxLayoutDirs = 24

// skipDirs are never walked: vendored code, build output and tool caches say
// nothing about how the repository is written, and they dominate the file
// counts when included. This list is the fallback path's defence — when the
// working copy is a git repo the file list comes from git instead, which
// applies the repository's own .gitignore and is always more accurate.
var skipDirs = map[string]bool{
	".git": true, ".claude": true, "worktrees": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	".next": true, ".nuxt": true, "out": true, "target": true, "Pods": true,
	".venv": true, "venv": true, "__pycache__": true, ".terraform": true,
	"coverage": true, ".idea": true, ".vscode": true, ".gradle": true,
	"DerivedData": true, ".dart_tool": true, ".svelte-kit": true, ".turbo": true,
	".pytest_cache": true, ".mypy_cache": true, "bin": true, "obj": true,
}

// languageByExt maps a file extension to the language name reported in the
// histogram. Extensions absent here are counted as files but not as a
// language — the histogram exists to name the stack, not to be exhaustive.
var languageByExt = map[string]string{
	".go": "Go", ".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript",
	".jsx": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".py": "Python", ".rb": "Ruby", ".rs": "Rust", ".java": "Java",
	".kt": "Kotlin", ".kts": "Kotlin", ".swift": "Swift", ".m": "Objective-C",
	".mm": "Objective-C", ".dart": "Dart", ".php": "PHP", ".cs": "C#",
	".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++", ".hpp": "C++",
	".scala": "Scala", ".ex": "Elixir", ".exs": "Elixir", ".sh": "Shell",
	".bash": "Shell", ".zsh": "Shell", ".sql": "SQL", ".vue": "Vue",
	".svelte": "Svelte", ".tf": "Terraform", ".proto": "Protobuf",
}

// Collect runs the whole deterministic pass over a working copy. It never
// fails the caller: an unreadable tree yields an empty Facts with a warning,
// because a missing fact block must degrade the profile, not block it.
func Collect(ctx context.Context, root string) Facts {
	f := Facts{Root: root, CollectedAt: time.Now().UTC()}
	if strings.TrimSpace(root) == "" {
		f.Warnings = append(f.Warnings, "no working copy path for this repository")
		return f
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		f.Warnings = append(f.Warnings, "working copy is missing at "+root)
		return f
	}

	tree := walk(ctx, root, &f)
	f.Languages = tree.languages()
	f.Layout = tree.layout()
	f.Migrations = tree.migrations

	collectManifests(root, tree, &f)
	collectWorkflows(root, tree, &f)
	collectPlatforms(root, tree, &f)
	collectTestAreas(root, tree, &f)
	collectGit(ctx, root, &f)
	// Deploy targets read both the workflows and the platform markers, so they
	// are derived last — a repo with a Vercel link AND a deploy workflow ships
	// through the workflow, and saying both without ranking them is how a
	// profile ends up advising the wrong one.
	deriveDeploys(&f)
	inferKind(tree, &f)
	return f
}

// treeScan is the single-pass result the rest of the collectors read from, so
// the tree is walked exactly once.
type treeScan struct {
	// files is every kept path, repo-relative, slash-separated.
	files []string
	// byName indexes basenames → repo-relative paths for marker lookups
	// (vercel.json, Dockerfile…) without re-walking.
	byName map[string][]string
	// dirFiles counts files per top-level (and per app/*) directory.
	dirFiles map[string]int
	// langFiles / langBytes accumulate the histogram.
	langFiles map[string]int
	langBytes map[string]int64
	// migrations are directories that look like a schema migration set.
	migrations []string
}

func (t *treeScan) has(path string) bool {
	for _, p := range t.byName[filepath.Base(path)] {
		if p == path {
			return true
		}
	}
	return false
}

// find returns the repo-relative paths of every file with the given basename.
func (t *treeScan) find(name string) []string { return t.byName[name] }

// findSuffix returns kept paths ending with the given suffix (cheap enough at
// walk-cap scale, and only used by the marker collectors).
func (t *treeScan) findSuffix(suffix string) []string {
	var out []string
	for _, p := range t.files {
		if strings.HasSuffix(p, suffix) {
			out = append(out, p)
		}
	}
	return out
}

func walk(ctx context.Context, root string, f *Facts) *treeScan {
	t := &treeScan{
		byName:    map[string][]string{},
		dirFiles:  map[string]int{},
		langFiles: map[string]int{},
		langBytes: map[string]int64{},
	}

	paths, viaGit := listTrackedFiles(ctx, root)
	if !viaGit {
		paths = walkFilesystem(ctx, root, f)
	}
	if len(paths) > maxWalkFiles {
		paths = paths[:maxWalkFiles]
		f.Truncated = true
	}

	seenMigrationDir := map[string]bool{}
	for _, rel := range paths {
		t.files = append(t.files, rel)
		base := filepath.Base(rel)
		t.byName[base] = append(t.byName[base], rel)
		t.dirFiles[areaOf(rel)]++

		if lang, ok := languageByExt[strings.ToLower(filepath.Ext(base))]; ok {
			t.langFiles[lang]++
			if info, err := os.Stat(filepath.Join(root, rel)); err == nil {
				t.langBytes[lang] += info.Size()
			}
		}
		if dir := migrationDirOf(rel); dir != "" && !seenMigrationDir[dir] {
			seenMigrationDir[dir] = true
			t.migrations = append(t.migrations, dir)
		}
	}

	f.FileCount = t.count()
	sort.Strings(t.migrations)
	return t
}

// listTrackedFiles asks git for the file list. It is the correct source and
// not merely the fast one: it applies the repository's own .gitignore, so
// build output, dependency trees and agent worktrees checked out inside the
// repo drop out without this package having to guess their names. A profile
// built from an ignore-blind walk described this very repository as eight
// duplicate Go modules, because eight agent worktrees lived under .claude/.
//
// The second return reports whether git answered; a non-git (or gitless)
// working copy falls back to the filesystem walk.
func listTrackedFiles(ctx context.Context, root string) ([]string, bool) {
	out, err := git(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, false
	}
	raw := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	paths := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// A submodule or nested checkout still lands here as a path; the skip
		// list keeps the obvious build/vendor cases out even under git.
		if skippedByPath(p) {
			continue
		}
		paths = append(paths, filepath.ToSlash(p))
	}
	if len(paths) == 0 {
		return nil, false
	}
	sort.Strings(paths)
	return paths, true
}

func skippedByPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

func walkFilesystem(ctx context.Context, root string, f *Facts) []string {
	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable subtree costs that subtree, not the walk
		}
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			// Only ".git" itself is skipped by prefix — ".github" holds the CI
			// workflows, which are half the deploy story.
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(paths) >= maxWalkFiles {
			f.Truncated = true
			return filepath.SkipAll
		}
		paths = append(paths, rel)
		return nil
	})
	return paths
}

func (t *treeScan) count() int { return len(t.files) }

// areaOf is the layout bucket a file belongs to. A monorepo's real structure
// lives one level down (apps/backend, packages/ui), so those get their own
// bucket instead of collapsing into "apps".
func areaOf(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) == 1 {
		return "" // repository root
	}
	switch parts[0] {
	case "apps", "packages", "services", "libs", "modules", "cmd":
		if len(parts) > 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	return parts[0]
}

// migrationDirOf recognises a schema-migration set from its file shape
// (0001_x.up.sql / 20240101_x.sql / V1__x.sql) rather than from the directory
// name alone, so a "migrations" folder of documentation is not mistaken for
// one and a "db/changes" folder of real migrations is not missed.
func migrationDirOf(rel string) string {
	dir, base := filepath.Split(rel)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return ""
	}
	lower := strings.ToLower(base)
	if !strings.HasSuffix(lower, ".sql") {
		return ""
	}
	if len(lower) < 4 {
		return ""
	}
	switch {
	case lower[0] >= '0' && lower[0] <= '9': // 0001_..., 20240101...
		return dir
	case strings.HasPrefix(lower, "v") && lower[1] >= '0' && lower[1] <= '9': // flyway V1__
		return dir
	case strings.Contains(strings.ToLower(dir), "migration"):
		return dir
	}
	return ""
}

func (t *treeScan) languages() []LanguageStat {
	out := make([]LanguageStat, 0, len(t.langFiles))
	for lang, n := range t.langFiles {
		out = append(out, LanguageStat{Language: lang, Files: n, Bytes: t.langBytes[lang]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Language < out[j].Language
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func (t *treeScan) layout() []DirNote {
	out := make([]DirNote, 0, len(t.dirFiles))
	for dir, n := range t.dirFiles {
		if dir == "" {
			continue
		}
		out = append(out, DirNote{Path: dir, Files: n, Role: roleOfDir(dir, t)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > maxLayoutDirs {
		out = out[:maxLayoutDirs]
	}
	return out
}

// roleOfDir labels a directory from what it contains, not from its name — the
// label is only useful when it survives a repo that names things its own way.
func roleOfDir(dir string, t *treeScan) string {
	if dir == ".github" {
		return "CI/CD workflows"
	}
	var go_, ts, swift, dart, sql, tf, yaml, docs int
	prefix := dir + "/"
	for _, p := range t.files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".go":
			go_++
		case ".ts", ".tsx", ".js", ".jsx", ".vue", ".svelte":
			ts++
		case ".swift":
			swift++
		case ".dart":
			dart++
		case ".sql":
			sql++
		case ".tf":
			tf++
		case ".yaml", ".yml":
			yaml++
		case ".md":
			docs++
		}
	}
	switch {
	case tf > 0 && tf >= yaml:
		return "infrastructure (terraform)"
	case yaml > 3 && go_+ts+swift+dart == 0:
		return "deployment manifests"
	case sql > 0 && go_+ts == 0:
		return "database schema"
	case swift > 0:
		return "iOS/Swift app"
	case dart > 0:
		return "Flutter app"
	case go_ > ts:
		return "Go service"
	case ts > 0:
		return "web frontend"
	case docs > 0:
		return "documentation"
	}
	return ""
}
