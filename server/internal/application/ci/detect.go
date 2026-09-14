// Package ci turns parsed GitHub Actions workflow jobs into per-category
// pipeline mapping suggestions, using a type-aware keyword set (e.g. a mobile
// repo never auto-picks a docker build).
package ci

import (
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// JobRef is one workflow job (neutral input, decoupled from the github adapter).
type JobRef struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	WorkflowFile string `json:"workflow_file"`
}

// CategorySuggestion is the resolved state of one (sub-repo, category) slot for
// the settings UI: an auto-pick when exactly one candidate matched, an
// ambiguity flag when 2+ matched, and the full candidate list for manual
// selection.
type CategorySuggestion struct {
	// SubProjectPath identifies which sub-project this slot is for ("" = the
	// repository itself). SubRepoKind is descriptive (which kind, for the
	// keyword set) — SubProjectPath is the identity that keeps two
	// same-kind sub-projects from colliding on one slot.
	SubProjectPath string      `json:"sub_project_path,omitempty"`
	SubRepoKind    string      `json:"sub_repo_kind"`
	Category       string      `json:"category"`
	TargetKind     string      `json:"target_kind"`
	Auto           string      `json:"auto"`      // auto-detected target_ref, "" if 0 or 2+ matches
	Ambiguous      bool        `json:"ambiguous"` // 2+ matched → user must pick
	Candidates     []Candidate `json:"candidates"`
}

// Candidate is one selectable target (a job name, or a workflow file).
type Candidate struct {
	Ref          string `json:"ref"`
	WorkflowFile string `json:"workflow_file,omitempty"`
}

// ConfigView is the settings payload for a repo's pipeline configuration: its
// type fields, whether GitHub workflows were found, per-slot auto-detect
// suggestions, and the currently saved mappings.
type ConfigView struct {
	Kind              string                         `json:"kind"`
	SubRepoKinds      []string                       `json:"sub_repo_kinds"`
	AutoReleaseOnDone bool                           `json:"auto_release_on_done"`
	HasWorkflows      bool                           `json:"has_workflows"`
	Suggestions       []CategorySuggestion           `json:"suggestions"`
	Saved             []domain.RepositoryPipelineJob `json:"saved"`
}

// jobCategories are the status-read categories (map to a run job).
var jobCategories = []string{
	domain.PipelineCategoryValidate,
	domain.PipelineCategoryBuild,
	domain.PipelineCategoryTest,
	domain.PipelineCategoryMutationTest,
}

// workflowCategories map to a workflow file rather than a job: the deploy
// categories are dispatched, pr_open is listed only (see the domain constant).
var workflowCategories = []string{
	domain.PipelineCategoryPROpen,
	domain.PipelineCategoryStageDeploy,
	domain.PipelineCategoryPreProdDeploy,
	domain.PipelineCategoryProdDeploy,
}

// keywords[category][kind] lists substrings that mark a job as that category
// for that repo kind. Deploy keywords are kind-independent.
var keywords = map[string]map[string][]string{
	// "validate" leads every list: a job named after the category itself is the
	// convention these repos now follow, and it must win over a tool name that
	// happens to appear in a longer job title.
	domain.PipelineCategoryValidate: {
		domain.RepoKindBackend:  {"validate", "lint", "vet", "staticcheck"},
		domain.RepoKindFrontend: {"validate", "lint", "tsc", "typecheck", "type-check"},
		domain.RepoKindMobile:   {"validate", "lint", "swiftlint", "analyze"},
		domain.RepoKindWorker:   {"validate", "lint", "vet", "staticcheck"},
	},
	domain.PipelineCategoryBuild: {
		domain.RepoKindBackend:  {"build", "compile", "docker"},
		domain.RepoKindFrontend: {"build"},
		domain.RepoKindMobile:   {"build", "xcodebuild", "archive", "fastlane"},
		domain.RepoKindWorker:   {"build", "compile", "docker"},
	},
	domain.PipelineCategoryTest: {
		domain.RepoKindBackend:  {"test"},
		domain.RepoKindFrontend: {"test", "vitest", "jest"},
		domain.RepoKindMobile:   {"test"},
		domain.RepoKindWorker:   {"test"},
	},
	// Mutation runners are named after the tool as often as after the
	// technique, so match both. Kind-independent in practice, but the map is
	// keyed by kind like the rest.
	domain.PipelineCategoryMutationTest: {
		domain.RepoKindBackend:  mutationKeywords,
		domain.RepoKindFrontend: mutationKeywords,
		domain.RepoKindMobile:   mutationKeywords,
		domain.RepoKindWorker:   mutationKeywords,
	},
}

var mutationKeywords = []string{"mutation", "mutant", "gremlins", "stryker", "mutmut", "pitest"}

// excludes[category][kind] rules out candidates even if a keyword matched — e.g.
// a mobile build must never auto-pick a docker job.
var excludes = map[string]map[string][]string{
	domain.PipelineCategoryBuild: {
		domain.RepoKindMobile: {"docker"},
	},
	// "mutation test" contains "test": without this the unit-test slot would
	// also match the mutation job, turning both into 2-candidate ambiguities
	// that force a manual pick on every repo.
	domain.PipelineCategoryTest: {
		domain.RepoKindBackend:  mutationKeywords,
		domain.RepoKindFrontend: mutationKeywords,
		domain.RepoKindMobile:   mutationKeywords,
		domain.RepoKindWorker:   mutationKeywords,
	},
}

var workflowKeywords = map[string][]string{
	domain.PipelineCategoryPROpen:        {"auto-pr", "auto_pr", "autopr", "open-pr", "pull-request"},
	domain.PipelineCategoryStageDeploy:   {"stage", "staging", "preview"},
	domain.PipelineCategoryPreProdDeploy: {"preprod", "pre-prod", "pre-production"},
	domain.PipelineCategoryProdDeploy:    {"prod", "production", "release"},
}

// workflowExcludes rules a workflow out of a category even if a keyword
// matched — the prod keyword "prod" is a substring of "preprod", so a preprod
// workflow must not also auto-pick the prod slot.
var workflowExcludes = map[string][]string{
	domain.PipelineCategoryProdDeploy: {"preprod", "pre-prod"},
}

// Suggest builds one CategorySuggestion per (sub-project, category).
// subProjects is nil/empty for single-kind repos (yields a single ""
// sub-project group whose keywords come from repoKind); for a monorepo it
// iterates the given sub-projects, one group per PATH (not per kind, so two
// sub-projects sharing a kind each get their own slots).
func Suggest(repoKind string, subProjects []domain.RepoSubProject, jobs []JobRef) []CategorySuggestion {
	groups := groupSubProjects(repoKind, subProjects)
	workflows := distinctWorkflows(jobs)

	var out []CategorySuggestion
	for _, g := range groups {
		for _, cat := range jobCategories {
			out = append(out, suggestJobCategory(g, cat, jobs))
		}
		for _, cat := range workflowCategories {
			out = append(out, suggestWorkflowCategory(g, cat, jobs, workflows))
		}
	}
	return out
}

type kindGroup struct {
	subProjectPath string // "" for the repository itself
	subRepoKind    string // "" for single-kind repos
	keywordKind    string // which keyword set to use
}

func groupSubProjects(repoKind string, subProjects []domain.RepoSubProject) []kindGroup {
	if repoKind != domain.RepoKindMonorepo {
		return []kindGroup{{keywordKind: repoKind}}
	}
	var groups []kindGroup
	for _, sp := range subProjects {
		if domain.ValidSubRepoKind(sp.Kind) {
			groups = append(groups, kindGroup{subProjectPath: sp.Path, subRepoKind: sp.Kind, keywordKind: sp.Kind})
		}
	}
	return groups
}

func suggestJobCategory(g kindGroup, category string, jobs []JobRef) CategorySuggestion {
	kws := keywords[category][g.keywordKind]
	exc := excludes[category][g.keywordKind]
	var matched []Candidate
	seen := map[string]bool{}
	for _, j := range jobs {
		if isReusableCall(j.Name) {
			continue
		}
		hay := strings.ToLower(j.Name + " " + j.Key)
		if containsAny(hay, exc) {
			continue
		}
		if containsAny(hay, kws) && !seen[j.Name] {
			seen[j.Name] = true
			matched = append(matched, Candidate{Ref: j.Name, WorkflowFile: j.WorkflowFile})
		}
	}
	return finalize(g, category, domain.PipelineTargetJob, matched)
}

func suggestWorkflowCategory(g kindGroup, category string, jobs []JobRef, workflows []Candidate) CategorySuggestion {
	kws := workflowKeywords[category]
	exc := workflowExcludes[category]
	var matched []Candidate
	seen := map[string]bool{}
	for _, w := range workflows {
		hay := strings.ToLower(w.Ref)
		if containsAny(hay, exc) {
			continue // e.g. a preprod workflow must not auto-pick the prod slot ("prod" ⊂ "preprod")
		}
		if containsAny(hay, kws) && !seen[w.Ref] {
			seen[w.Ref] = true
			matched = append(matched, w)
		}
	}
	return finalize(g, category, domain.PipelineTargetWorkflow, matched)
}

func finalize(g kindGroup, category, targetKind string, matched []Candidate) CategorySuggestion {
	s := CategorySuggestion{
		SubProjectPath: g.subProjectPath,
		SubRepoKind:    g.subRepoKind,
		Category:       category,
		TargetKind:     targetKind,
		Candidates:     matched,
	}
	switch len(matched) {
	case 0:
		// leave unset — category is skipped by the pipeline
	case 1:
		s.Auto = matched[0].Ref
	default:
		s.Ambiguous = true
	}
	return s
}

func distinctWorkflows(jobs []JobRef) []Candidate {
	seen := map[string]bool{}
	var out []Candidate
	for _, j := range jobs {
		if j.WorkflowFile == "" || seen[j.WorkflowFile] {
			continue
		}
		seen[j.WorkflowFile] = true
		out = append(out, Candidate{Ref: j.WorkflowFile, WorkflowFile: j.WorkflowFile})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

// isReusableCall reports whether a job name is GitHub's composite name for a
// job reached through `uses: ./.github/workflows/x.yml` — "caller / callee".
// Those are the SAME jobs the called workflow already reported, so counting
// them would turn every category into a 2-candidate ambiguity and force a
// manual pick on repos whose mapping is otherwise unambiguous. A plain job name
// cannot contain " / ": GitHub reserves the separator for this.
func isReusableCall(name string) bool {
	return strings.Contains(name, " / ")
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
