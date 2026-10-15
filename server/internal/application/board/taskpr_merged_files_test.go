package board

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestMergedTaskFilesListsEveryPathARenameTouched(t *testing.T) {
	task := mergeTask()
	svc, _, _, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{})
	svc.prs = &mergePRs{files: []port.PullRequestFile{
		{Path: "docs/tasks.md"},
		{Path: "web/new.tsx", PreviousPath: "api/old.tsx"},
	}}

	files, err := svc.MergedTaskFiles(context.Background(), repositoryID, task)

	require.NoError(t, err)
	assert.Equal(t, []string{"docs/tasks.md", "web/new.tsx", "api/old.tsx"}, files)
}

func TestMergedTaskFilesRefusesAListThatMayBeCutShort(t *testing.T) {
	task := mergeTask()
	svc, _, _, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{})
	files := make([]port.PullRequestFile, mergedFilesCap)
	for i := range files {
		files[i] = port.PullRequestFile{Path: fmt.Sprintf("docs/%d.md", i)}
	}
	svc.prs = &mergePRs{files: files}

	_, err := svc.MergedTaskFiles(context.Background(), repositoryID, task)

	require.ErrorIs(t, err, errMergedFilesTruncated)
}

func TestMergedTaskFilesNeedsAPullRequest(t *testing.T) {
	task := mergeTask()
	task.PRURL, task.PRNumber = "", 0
	svc, _, _, repositoryID := newMergeFixture(task, openCleanPR(), &mergeGates{})

	_, err := svc.MergedTaskFiles(context.Background(), repositoryID, task)

	require.Error(t, err)
}
