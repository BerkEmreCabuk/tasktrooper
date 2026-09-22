package ci

import (
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type JobRef struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	WorkflowFile string `json:"workflow_file"`
}

type CategorySuggestion struct {
	SubProjectPath string      `json:"sub_project_path,omitempty"`
	SubRepoKind    string      `json:"sub_repo_kind"`
	Category       string      `json:"category"`
	TargetKind     string      `json:"target_kind"`
	Auto           string      `json:"auto"`
	Ambiguous      bool        `json:"ambiguous"`
	Candidates     []Candidate `json:"candidates"`
}

type Candidate struct {
	Ref          string `json:"ref"`
	WorkflowFile string `json:"workflow_file,omitempty"`
}

type ConfigView struct {
	Kind              string                         `json:"kind"`
	SubRepoKinds      []string                       `json:"sub_repo_kinds"`
	AutoReleaseOnDone bool                           `json:"auto_release_on_done"`
	HasWorkflows      bool                           `json:"has_workflows"`
	Suggestions       []CategorySuggestion           `json:"suggestions"`
	Saved             []domain.RepositoryPipelineJob `json:"saved"`
}

var jobCategories = []string{
	domain.PipelineCategoryValidate,
	domain.PipelineCategoryBuild,
	domain.PipelineCategoryTest,
	domain.PipelineCategoryMutationTest,
}

var workflowCategories = []string{
	domain.PipelineCategoryPROpen,
	domain.PipelineCategoryStageDeploy,
	domain.PipelineCategoryPreProdDeploy,
	domain.PipelineCategoryProdDeploy,
}

var keywords = map[string]map[string][]string{

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

	domain.PipelineCategoryMutationTest: {
		domain.RepoKindBackend:  mutationKeywords,
		domain.RepoKindFrontend: mutationKeywords,
		domain.RepoKindMobile:   mutationKeywords,
		domain.RepoKindWorker:   mutationKeywords,
	},
}

var mutationKeywords = []string{"mutation", "mutant", "gremlins", "stryker", "mutmut", "pitest"}

var excludes = map[string]map[string][]string{
	domain.PipelineCategoryBuild: {
		domain.RepoKindMobile: {"docker"},
	},

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

var workflowExcludes = map[string][]string{
	domain.PipelineCategoryProdDeploy: {"preprod", "pre-prod"},
}

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
	subProjectPath string
	subRepoKind    string
	keywordKind    string
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
			continue
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
