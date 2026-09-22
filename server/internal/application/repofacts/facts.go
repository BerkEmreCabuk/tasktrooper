// Package repofacts collects the deterministic half of a repository's profile — the facts a program reads straight off the working copy.
// Parser-established facts must never be left to a model: a profiling run asked for build/test/deploy answered with its priors for a repository whose real answer was a Go module, a pnpm workspace and a GKE deploy. The agent gets this as given and its job shrinks to the judgment a parser cannot make. Every claim carries the repo-relative path it was read from, so the profile can be audited against the tree that produced it.
package repofacts

import "time"

// One derived claim plus the paths that back it; evidence is repo-relative and may carry a ":line" suffix.
type Fact struct {
	Text     string   `json:"text"`
	Evidence []string `json:"evidence,omitempty"`
}

// One row of the language histogram, counted over the files the walk kept (vendored and generated trees are excluded — see skipDirs).
type LanguageStat struct {
	Language string `json:"language"`
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
}

// Dependency/BUILD manifest found in the tree; Deps is the short framework-defining list, not the full lockfile, so the profile says "React 19 + Vite" without the agent opening anything.
type Manifest struct {
	Path      string   `json:"path"`
	Ecosystem string   `json:"ecosystem"` // npm | go | python | rust | dart | swift | ruby | java | php
	Name      string   `json:"name,omitempty"`
	Version   string   `json:"version,omitempty"` // language/runtime version when the manifest pins one
	Manager   string   `json:"manager,omitempty"` // resolved from the lockfile that sits next to it
	Deps      []string `json:"deps,omitempty"`
	Workspace []string `json:"workspace,omitempty"` // npm/pnpm workspace globs
}

// A runnable command the tree actually declares; Source is the path it was read from — a command with no source is not a fact and never enters this list.
type Command struct {
	Purpose string `json:"purpose"` // build | test | run | lint | typecheck | migrate
	Area    string `json:"area"`    // repo-relative dir the command belongs to ("" = root)
	Cmd     string `json:"cmd"`
	Source  string `json:"source"`
}

// One top-level (or app-level) directory with the file count and the role inferred from what is inside it.
type DirNote struct {
	Path  string `json:"path"`
	Role  string `json:"role,omitempty"`
	Files int    `json:"files"`
}

// A parsed .github/workflows file: what fires it and what it runs. Triggers are rendered event strings ("push:main", "pull_request", "workflow_dispatch", "schedule:0 3 * * *").
type Workflow struct {
	File         string   `json:"file"`
	Name         string   `json:"name,omitempty"`
	Triggers     []string `json:"triggers,omitempty"`
	Jobs         []string `json:"jobs,omitempty"`
	Dispatchable bool     `json:"dispatchable"`
	// Marks a workflow whose steps push somewhere (deploy/release/publish keywords, or a known deploy action) — what turns "there is a workflow" into "this is how the repo ships".
	Deploys bool `json:"deploys,omitempty"`
}

// Answers the question the old free-text profile kept fumbling: what makes this repository ship, and where to.
type DeployTarget struct {
	Provider    string `json:"provider"`
	Environment string `json:"environment,omitempty"`
	Trigger     string `json:"trigger"`
	Evidence    string `json:"evidence"`
	// True when the trigger needs no human action beyond landing a commit — the "committing deploys" case worth stating loudly.
	Automatic bool `json:"automatic"`
}

// A third-party service the repo is wired to (hosting, error tracking, analytics, auth, payments…); Category groups them in the profile.
type Integration struct {
	Name     string `json:"name"`
	Category string `json:"category"` // hosting | ci | observability | auth | data | payments | mobile | infra
	Detail   string `json:"detail,omitempty"`
	Evidence string `json:"evidence"`
}

type Hotspot struct {
	Path    string `json:"path"`
	Commits int    `json:"commits"`
}

// How the repository is worked, read from real history rather than from a CONTRIBUTING file nobody follows.
type GitFacts struct {
	DefaultBranch string `json:"default_branch,omitempty"`
	RemoteHost    string `json:"remote_host,omitempty"` // github.com, gitlab.com…
	RemoteSlug    string `json:"remote_slug,omitempty"` // owner/repo
	// The dominant naming shape of recent branches ("feature/*", "<type>/<slug>", "flat") with the sample size behind it.
	BranchPattern   string    `json:"branch_pattern,omitempty"`
	BranchSamples   []string  `json:"branch_samples,omitempty"`
	MergeStyle      string    `json:"merge_style,omitempty"` // merge-commits | squash/rebase (linear)
	CommitStyle     string    `json:"commit_style,omitempty"`
	DirectToMain    bool      `json:"direct_to_main"` // recent history lands without PR merges
	Hotspots        []Hotspot `json:"hotspots,omitempty"`
	HeadSHA         string    `json:"head_sha,omitempty"`
	HeadCommittedAt time.Time `json:"head_committed_at,omitempty"`
}

type TestArea struct {
	Area      string `json:"area"`
	Framework string `json:"framework"`
	Command   string `json:"command,omitempty"`
	Evidence  string `json:"evidence"`
}

// The whole deterministic pass: rendered into the profiling prompt as given-truth and stored as the "derived" profile sections.
type Facts struct {
	Root        string    `json:"-"`
	CollectedAt time.Time `json:"collected_at"`

	// The inferred repository classification, offered to the pipeline settings as a proposal.
	Kind         string   `json:"kind,omitempty"`
	SubKinds     []string `json:"sub_kinds,omitempty"`
	KindEvidence []string `json:"kind_evidence,omitempty"`

	Languages    []LanguageStat `json:"languages,omitempty"`
	Manifests    []Manifest     `json:"manifests,omitempty"`
	Commands     []Command      `json:"commands,omitempty"`
	Layout       []DirNote      `json:"layout,omitempty"`
	Workflows    []Workflow     `json:"workflows,omitempty"`
	Deploys      []DeployTarget `json:"deploys,omitempty"`
	Integrations []Integration  `json:"integrations,omitempty"`
	Migrations   []string       `json:"migrations,omitempty"`
	TestAreas    []TestArea     `json:"test_areas,omitempty"`
	Git          GitFacts       `json:"git"`

	// How much of the tree the walk covered, so a profile built from a partial walk never reads as exhaustive.
	FileCount int  `json:"file_count"`
	Truncated bool `json:"truncated,omitempty"`

	// Collection failures worth surfacing (unreadable manifest, git unavailable) rather than silently dropping.
	Warnings []string `json:"warnings,omitempty"`
}

// An empty Facts (unreadable or missing working copy) must not be injected as "verified facts": an empty fact block reads as "nothing here", worse than no block at all.
func (f Facts) HasAny() bool {
	return f.FileCount > 0 || len(f.Manifests) > 0 || len(f.Workflows) > 0 || len(f.Integrations) > 0
}

func (f Facts) commandsFor(purpose string) []Command {
	var out []Command
	for _, c := range f.Commands {
		if c.Purpose == purpose {
			out = append(out, c)
		}
	}
	return out
}
