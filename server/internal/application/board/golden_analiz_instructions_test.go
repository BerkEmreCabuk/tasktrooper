package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Migration 166 cleared workflow_stages.instructions for every analiz stage
// that migration 143 had seeded (todo, in_progress, need_revision,
// analiz_review, done): the full prompt now lives in catalog/agents/
// system-architect's column md files instead of the database. This test
// keeps workflowtest's default workflow honest against that cleared state;
// TestSystemArchitectCatalogCarriesTheAnalizWorkflow (catalog_content_test.go)
// checks the prompt actually landed in the catalog.
func TestGoldenAnalizStageInstructionsAreClearedByMigration166(t *testing.T) {
	wf := workflowtest.Default().Workflows[domain.TaskType("analiz")]

	for _, stage := range wf.Stages {
		if stage.Instructions != "" {
			t.Fatalf("analiz column %s still carries stage instructions after migration 166 cleared them: %q", stage.Column, stage.Instructions)
		}
	}
}

func TestGoldenNonAnalizStageInstructionsAreEmpty(t *testing.T) {
	for _, taskType := range []domain.TaskType{
		domain.TaskType("task"), domain.TaskType("bug"), domain.TaskType("technical"),
	} {
		wf := workflowtest.Default().Workflows[taskType]
		for _, stage := range wf.Stages {
			if stage.Instructions != "" {
				t.Fatalf("%s column %s carries stage instructions, but taskTypeInstruction never produced any for a non-analiz type: %q",
					taskType, stage.Column, stage.Instructions)
			}
		}
	}
}
