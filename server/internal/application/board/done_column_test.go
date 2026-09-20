package board

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestDoneRunNeverCommitsTheWorkspace(t *testing.T) {
	assert.False(t, producesADiff(taskWF, domain.TaskColumnDone),
		"a run in done merges a finished change; committing its workspace would re-create the merged branch")

	for _, column := range []domain.TaskColumn{
		domain.TaskColumnInProgress,
		domain.TaskColumnNeedRevision,
		domain.TaskColumnTodo,
		domain.TaskColumnInQA,
	} {
		assert.True(t, producesADiff(taskWF, column), "%s must still commit and push", column)
	}

	for _, column := range []domain.TaskColumn{
		domain.TaskColumnCodeReview,
		domain.TaskColumnAnalizReview,
		domain.TaskColumnPMUAT,
	} {
		assert.False(t, producesADiff(taskWF, column))
	}
}

func TestDoneInstructionIsAboutMergingAndNeverAboutReleasing(t *testing.T) {
	instruction := columnInstruction(taskWF, domain.BoardTask{
		Column:   domain.TaskColumnDone,
		TaskType: "task",
	})

	assert.Contains(t, instruction, "merge_task_pull_request")
	assert.NotContains(t, instruction, "move the task on to the next column")
	assert.Contains(t, instruction, "do NOT move this task to `released`")
}

func TestDoneInstructionForAnalizIsUnchanged(t *testing.T) {
	instruction := columnInstruction(analizWF, domain.BoardTask{
		Column:   domain.TaskColumnDone,
		TaskType: "analiz",
	})

	assert.Contains(t, instruction, "decompose")
	assert.NotContains(t, instruction, "merge_task_pull_request")
}
