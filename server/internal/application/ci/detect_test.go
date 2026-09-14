package ci

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func find(suggestions []CategorySuggestion, subRepo, category string) CategorySuggestion {
	for _, s := range suggestions {
		if s.SubRepoKind == subRepo && s.Category == category {
			return s
		}
	}
	return CategorySuggestion{}
}

func TestSuggest_BackendSingleKind(t *testing.T) {
	jobs := []JobRef{
		{Key: "lint", Name: "lint", WorkflowFile: "ci.yml"},
		{Key: "build", Name: "build", WorkflowFile: "ci.yml"},
		{Key: "test", Name: "test", WorkflowFile: "ci.yml"},
	}
	got := Suggest(domain.RepoKindBackend, nil, jobs)

	if v := find(got, "", domain.PipelineCategoryValidate); v.Auto != "lint" {
		t.Errorf("validate auto = %q, want lint", v.Auto)
	}
	if v := find(got, "", domain.PipelineCategoryBuild); v.Auto != "build" {
		t.Errorf("build auto = %q, want build", v.Auto)
	}
	if v := find(got, "", domain.PipelineCategoryTest); v.Auto != "test" {
		t.Errorf("test auto = %q, want test", v.Auto)
	}
}

func TestSuggest_MobileNeverPicksDocker(t *testing.T) {
	jobs := []JobRef{
		{Key: "docker-build", Name: "docker-build", WorkflowFile: "ci.yml"},
		{Key: "xcode", Name: "xcodebuild", WorkflowFile: "ios.yml"},
	}
	got := Suggest(domain.RepoKindMobile, nil, jobs)
	build := find(got, "", domain.PipelineCategoryBuild)
	if build.Auto != "xcodebuild" {
		t.Errorf("mobile build auto = %q, want xcodebuild (docker excluded)", build.Auto)
	}
	for _, c := range build.Candidates {
		if c.Ref == "docker-build" {
			t.Errorf("mobile build candidates must exclude docker-build, got %v", build.Candidates)
		}
	}
}

func TestSuggest_AmbiguousLeavesUnset(t *testing.T) {
	jobs := []JobRef{
		{Key: "test-unit", Name: "test-unit", WorkflowFile: "ci.yml"},
		{Key: "test-integration", Name: "test-integration", WorkflowFile: "ci.yml"},
	}
	got := Suggest(domain.RepoKindBackend, nil, jobs)
	test := find(got, "", domain.PipelineCategoryTest)
	if !test.Ambiguous || test.Auto != "" {
		t.Errorf("expected ambiguous test with no auto, got auto=%q ambiguous=%v", test.Auto, test.Ambiguous)
	}
	if len(test.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(test.Candidates))
	}
}

func TestSuggest_NoMatchSkips(t *testing.T) {
	jobs := []JobRef{{Key: "deploy", Name: "deploy", WorkflowFile: "deploy.yml"}}
	got := Suggest(domain.RepoKindBackend, nil, jobs)
	v := find(got, "", domain.PipelineCategoryValidate)
	if v.Auto != "" || v.Ambiguous || len(v.Candidates) != 0 {
		t.Errorf("expected empty validate slot, got %+v", v)
	}
}

func TestSuggest_DeployWorkflows(t *testing.T) {
	jobs := []JobRef{
		{Key: "deploy", Name: "deploy", WorkflowFile: "deploy-staging.yml"},
		{Key: "deploy", Name: "deploy", WorkflowFile: "deploy-production.yml"},
	}
	got := Suggest(domain.RepoKindBackend, nil, jobs)
	stage := find(got, "", domain.PipelineCategoryStageDeploy)
	if stage.TargetKind != domain.PipelineTargetWorkflow || stage.Auto != "deploy-staging.yml" {
		t.Errorf("stage deploy auto = %q (kind %q), want deploy-staging.yml/workflow", stage.Auto, stage.TargetKind)
	}
	prod := find(got, "", domain.PipelineCategoryProdDeploy)
	if prod.Auto != "deploy-production.yml" {
		t.Errorf("prod deploy auto = %q, want deploy-production.yml", prod.Auto)
	}
}

func TestSuggest_PreprodDeployExcludedFromProd(t *testing.T) {
	jobs := []JobRef{
		{Key: "deploy", Name: "deploy", WorkflowFile: "deploy-staging.yml"},
		{Key: "deploy", Name: "deploy", WorkflowFile: "deploy-preprod.yml"},
		{Key: "deploy", Name: "deploy", WorkflowFile: "deploy-production.yml"},
	}
	got := Suggest(domain.RepoKindBackend, nil, jobs)

	preprod := find(got, "", domain.PipelineCategoryPreProdDeploy)
	if preprod.Auto != "deploy-preprod.yml" {
		t.Errorf("preprod auto = %q, want deploy-preprod.yml", preprod.Auto)
	}
	// "prod" ⊂ "preprod", so the preprod workflow must NOT bleed into the prod slot.
	prod := find(got, "", domain.PipelineCategoryProdDeploy)
	if prod.Auto != "deploy-production.yml" {
		t.Errorf("prod auto = %q, want deploy-production.yml (preprod excluded)", prod.Auto)
	}
	for _, c := range prod.Candidates {
		if c.Ref == "deploy-preprod.yml" {
			t.Errorf("prod candidates must exclude preprod, got %v", prod.Candidates)
		}
	}
	if stage := find(got, "", domain.PipelineCategoryStageDeploy); stage.Auto != "deploy-staging.yml" {
		t.Errorf("stage auto = %q, want deploy-staging.yml", stage.Auto)
	}
}

func TestSuggest_MonorepoPerSubKind(t *testing.T) {
	jobs := []JobRef{
		{Key: "backend-test", Name: "backend-test", WorkflowFile: "ci.yml"},
		{Key: "frontend-vitest", Name: "frontend-vitest", WorkflowFile: "ci.yml"},
	}
	got := Suggest(domain.RepoKindMonorepo, []domain.RepoSubProject{
		{Path: "apps/backend", Kind: domain.RepoKindBackend},
		{Path: "apps/frontend", Kind: domain.RepoKindFrontend},
	}, jobs)
	// backend test should match backend-test (keyword "test")
	bt := find(got, domain.RepoKindBackend, domain.PipelineCategoryTest)
	ft := find(got, domain.RepoKindFrontend, domain.PipelineCategoryTest)
	if bt.SubRepoKind != domain.RepoKindBackend {
		t.Errorf("expected backend sub-repo test slot present")
	}
	if ft.SubRepoKind != domain.RepoKindFrontend {
		t.Errorf("expected frontend sub-repo test slot present")
	}
}

// The settings UI computes suggestions over every sub-repo kind (not just the
// saved ones) so a freshly ticked sub-project shows its candidate jobs before
// the selection is persisted. Guards the GetPipelineConfig call site.
func TestSuggest_MonorepoAllKindsForUnsavedSelection(t *testing.T) {
	jobs := []JobRef{
		{Key: "worker-test", Name: "worker-test", WorkflowFile: "worker.yml"},
	}
	var allKinds []domain.RepoSubProject
	for _, k := range domain.AllSubRepoKinds() {
		allKinds = append(allKinds, domain.RepoSubProject{Kind: k})
	}
	got := Suggest(domain.RepoKindMonorepo, allKinds, jobs)
	wt := find(got, domain.RepoKindWorker, domain.PipelineCategoryTest)
	if wt.SubRepoKind != domain.RepoKindWorker {
		t.Fatalf("expected worker test slot present without worker being saved")
	}
	if wt.Auto != "worker-test" {
		t.Errorf("worker test auto = %q, want worker-test", wt.Auto)
	}
}

// The split CI layout this codebase actually ships: four jobs in ci.yml plus an
// auto-pr.yml that reuses them. Every category must land on exactly one target
// with no manual pick left over.
func TestSuggest_SplitPipelineWithAutoPR(t *testing.T) {
	jobs := []JobRef{
		{Key: "validate", Name: "validate", WorkflowFile: "ci.yml"},
		{Key: "build", Name: "build", WorkflowFile: "ci.yml"},
		{Key: "test", Name: "test", WorkflowFile: "ci.yml"},
		{Key: "mutation-test", Name: "mutation-test", WorkflowFile: "ci.yml"},
		// Composite names: what GitHub reports for jobs reached through
		// `uses: ./.github/workflows/ci.yml`. Same jobs, second listing.
		{Key: "ci", Name: "ci / validate", WorkflowFile: "auto-pr.yml"},
		{Key: "ci", Name: "ci / build", WorkflowFile: "auto-pr.yml"},
		{Key: "ci", Name: "ci / test", WorkflowFile: "auto-pr.yml"},
		{Key: "ci", Name: "ci / mutation-test", WorkflowFile: "auto-pr.yml"},
		{Key: "open-pr", Name: "open-pr", WorkflowFile: "auto-pr.yml"},
		{Key: "release", Name: "release", WorkflowFile: "release.yml"},
	}
	got := Suggest(domain.RepoKindBackend, nil, jobs)

	for _, tc := range []struct{ category, want string }{
		{domain.PipelineCategoryValidate, "validate"},
		{domain.PipelineCategoryBuild, "build"},
		{domain.PipelineCategoryTest, "test"},
		{domain.PipelineCategoryMutationTest, "mutation-test"},
		{domain.PipelineCategoryPROpen, "auto-pr.yml"},
		{domain.PipelineCategoryProdDeploy, "release.yml"},
	} {
		s := find(got, "", tc.category)
		if s.Auto != tc.want {
			t.Errorf("%s auto = %q, want %q", tc.category, s.Auto, tc.want)
		}
		if s.Ambiguous {
			t.Errorf("%s is ambiguous; candidates: %+v", tc.category, s.Candidates)
		}
	}
}

// "mutation test" contains "test": without the exclude, the unit-test slot
// matches the mutation job too and both categories go ambiguous.
func TestSuggest_MutationJobDoesNotClaimTheTestSlot(t *testing.T) {
	jobs := []JobRef{
		{Key: "test", Name: "test", WorkflowFile: "ci.yml"},
		{Key: "mutation-test", Name: "mutation-test", WorkflowFile: "ci.yml"},
	}
	got := Suggest(domain.RepoKindBackend, nil, jobs)

	unit := find(got, "", domain.PipelineCategoryTest)
	if unit.Auto != "test" || unit.Ambiguous {
		t.Errorf("test auto = %q ambiguous = %v, want test/false", unit.Auto, unit.Ambiguous)
	}
	mut := find(got, "", domain.PipelineCategoryMutationTest)
	if mut.Auto != "mutation-test" || mut.Ambiguous {
		t.Errorf("mutation_test auto = %q ambiguous = %v, want mutation-test/false", mut.Auto, mut.Ambiguous)
	}
}
