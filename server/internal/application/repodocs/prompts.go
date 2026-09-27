package repodocs

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// docTaskInput is also what partial.repodocs_doc_instructions and
// partial.repodocs_local_run_script_requirements expect: Kind and FullPath
// are shared field names so the same struct value can be handed to a
// partial call unchanged.
type docTaskInput struct {
	Kind      string
	FullPath  string
	KindLabel string
}

var docTaskKey = prompt.Define[docTaskInput]("briefs.repodocs.doc_task", docTaskInput{
	Kind: "coding_standards", FullPath: ".ai/coding-standards.md", KindLabel: "backend",
})

type newRepoDocInstructionsInput struct {
	Kind     string
	FullPath string
}

var newRepoDocInstructionsKey = prompt.Define[newRepoDocInstructionsInput]("briefs.repodocs.new_repo_doc_instructions", newRepoDocInstructionsInput{
	Kind: "coding_standards", FullPath: ".ai/coding-standards.md",
})

type bundleDocInput struct {
	Number     int
	FullPath   string
	KindLabel  string
	ScopeLabel string
	Kind       string
}

type docsBundleInput struct {
	RepoKind string
	Docs     []bundleDocInput
}

var docsBundleKey = prompt.Define[docsBundleInput]("briefs.repodocs.docs_bundle", docsBundleInput{
	RepoKind: "backend",
	Docs: []bundleDocInput{
		{Number: 1, FullPath: ".ai/coding-standards.md", KindLabel: "coding standards", ScopeLabel: "the repository (backend)", Kind: "coding_standards"},
	},
})
