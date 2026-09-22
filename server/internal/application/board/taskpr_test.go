package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type taskPRGit struct {
	hasGit     bool
	headSHAs   []string
	branch     string
	commitErr  error
	commits    []string
	prURL      string
	prErr      error
	changed    []string
	infoCalls  int
	prPushCall int

	mergeReqs   []domain.PullRequestMergeRequest
	mergeResult domain.PullRequestMergeResult
	mergeErr    error
}

func (g *taskPRGit) HasGit(string) bool { return g.hasGit }

func (g *taskPRGit) CommitAndPush(_ context.Context, _, message string) error {
	if g.commitErr != nil {
		return g.commitErr
	}
	g.commits = append(g.commits, message)
	return nil
}

func (g *taskPRGit) EnsurePullRequest(context.Context, string) (string, error) {
	g.prPushCall++
	return g.prURL, g.prErr
}

func (g *taskPRGit) MergePullRequest(_ context.Context, req domain.PullRequestMergeRequest) (domain.PullRequestMergeResult, error) {
	g.mergeReqs = append(g.mergeReqs, req)
	if g.mergeErr != nil {
		return domain.PullRequestMergeResult{}, g.mergeErr
	}
	result := g.mergeResult
	if result.MergeCommitSHA == "" {
		result.MergeCommitSHA = "mergecommitsha0000000000000000000000000"
	}
	result.Undrafted = req.Undraft
	if req.DeleteBranch && result.BranchDeleteError == "" {
		result.BranchDeleted = true
	}
	return result, nil
}

func (g *taskPRGit) TaskGitInfo(context.Context, string) (domain.TaskGitInfo, error) {
	sha := ""
	if g.infoCalls < len(g.headSHAs) {
		sha = g.headSHAs[g.infoCalls]
	} else if len(g.headSHAs) > 0 {
		sha = g.headSHAs[len(g.headSHAs)-1]
	}
	g.infoCalls++
	return domain.TaskGitInfo{Owner: "acme", Repo: "widget", Branch: g.branch, HeadSHA: sha}, nil
}

func (g *taskPRGit) TaskChangedFiles(context.Context, string) ([]string, error) {
	return g.changed, nil
}

func (g *taskPRGit) OriginURL(context.Context, string) string {
	return "https://github.com/acme/widget.git"
}

func newCommitFixture(task domain.BoardTask, repositoryID uuid.UUID, git *taskPRGit) (*TaskPRService, *taskChatTaskStore) {
	tasks := &taskChatTaskStore{tasks: map[[2]uuid.UUID]domain.BoardTask{{repositoryID, task.ID}: task}}
	return NewTaskPRService(TaskPRServiceDeps{
		Tasks:         tasks,
		Repos:         taskChatRepos{root: "/repos/widget"},
		Git:           git,
		Tokens:        func(context.Context) (string, error) { return "tok", nil },
		WorkspaceRoot: "/data/workspaces",
	}), tasks
}

func TestCommitTaskChangesPushesAndRecordsThePR(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link"}
	git := &taskPRGit{
		hasGit:   true,
		headSHAs: []string{"aaa1111", "bbb2222"},
		branch:   "feature/task-1234abcd-add-the-store-link",
		prURL:    "https://github.com/acme/widget/pull/42",
		changed:  []string{"apps/web/src/App.tsx"},
	}
	svc, tasks := newCommitFixture(task, repositoryID, git)

	result, err := svc.CommitTaskChanges(context.Background(), repositoryID, task.ID, "add the store link to the footer")
	require.NoError(t, err)

	assert.True(t, result.Committed)
	assert.Equal(t, []string{"add the store link to the footer\n\nTask: DE-1\n"}, git.commits)
	assert.Equal(t, "bbb2222", result.SHA)
	assert.Equal(t, "feature/task-1234abcd-add-the-store-link", result.Branch)
	assert.Equal(t, "https://github.com/acme/widget/pull/42", result.PRURL)
	assert.Equal(t, 42, result.PRNumber)
	assert.Equal(t, []string{"apps/web/src/App.tsx"}, result.ChangedFiles)
	assert.Contains(t, result.Message, "pull request")
	assert.Equal(t, "https://github.com/acme/widget/pull/42", tasks.prs[task.ID])
}

func TestCommitTaskChangesReportsNothingToCommit(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link"}
	git := &taskPRGit{
		hasGit:   true,
		headSHAs: []string{"aaa1111", "aaa1111"},
		branch:   "feature/task-1234abcd-add-the-store-link",
		prURL:    "https://github.com/acme/widget/pull/42",
	}
	svc, _ := newCommitFixture(task, repositoryID, git)

	result, err := svc.CommitTaskChanges(context.Background(), repositoryID, task.ID, "no-op")
	require.NoError(t, err)

	assert.False(t, result.Committed)
	assert.Contains(t, result.Message, "Nothing to commit")
	assert.Equal(t, "https://github.com/acme/widget/pull/42", result.PRURL, "the existing PR is still named")
}

func TestCommitTaskChangesWithoutAWorkspaceIsANoOp(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link"}
	git := &taskPRGit{hasGit: false}
	svc, _ := newCommitFixture(task, repositoryID, git)

	result, err := svc.CommitTaskChanges(context.Background(), repositoryID, task.ID, "anything")
	require.NoError(t, err)

	assert.False(t, result.Committed)
	assert.Contains(t, result.Message, "no working copy")
	assert.Empty(t, git.commits)
	assert.Zero(t, git.prPushCall, "nothing is pushed and no PR is opened for a branch that does not exist")
}

func TestCommitTaskChangesSurvivesAFailedPROpen(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link"}
	git := &taskPRGit{
		hasGit:   true,
		headSHAs: []string{"aaa1111", "bbb2222"},
		prErr:    errors.New("No commits between main and feature/x"),
	}
	svc, _ := newCommitFixture(task, repositoryID, git)

	result, err := svc.CommitTaskChanges(context.Background(), repositoryID, task.ID, "wip")
	require.NoError(t, err)
	assert.True(t, result.Committed)
	assert.Empty(t, result.PRURL)
	assert.Contains(t, result.Message, "No pull request")
}

func TestPullRequestSaysSoWhenNoPRIsKnown(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link"}
	svc, _ := newCommitFixture(task, repositoryID, &taskPRGit{hasGit: true})

	pr, err := svc.PullRequest(context.Background(), repositoryID, task.ID, true)
	require.NoError(t, err)

	assert.False(t, pr.Known)
	assert.Zero(t, pr.Number)
	assert.Contains(t, pr.Note, "No pull request has been opened")
}

func TestPullRequestKeepsAnUnparsableLink(t *testing.T) {
	repositoryID := uuid.New()
	task := domain.BoardTask{
		ID: uuid.New(), RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link",
		PRURL: "https://github.enterprise.local/acme/widget/merge_requests/x",
	}
	svc, _ := newCommitFixture(task, repositoryID, &taskPRGit{hasGit: true})

	pr, err := svc.PullRequest(context.Background(), repositoryID, task.ID, true)
	require.NoError(t, err)

	assert.True(t, pr.Known)
	assert.Zero(t, pr.Number)
	assert.Equal(t, task.PRURL, pr.URL)
	assert.Contains(t, pr.Note, "number could not be read")
}

func TestRecordTaskPRStoresTheLinkEvenWhenTheNumberDoesNotParse(t *testing.T) {
	tasks := &taskChatTaskStore{}
	taskID := uuid.New()

	recordTaskPR(context.Background(), tasks, taskID, "https://example.invalid/not-a-pr")
	assert.Equal(t, "https://example.invalid/not-a-pr", tasks.prs[taskID])

	recordTaskPR(context.Background(), tasks, taskID, " https://github.com/acme/widget/pull/9 ")
	assert.Equal(t, "https://github.com/acme/widget/pull/9", tasks.prs[taskID], "the stored URL is trimmed")

	recordTaskPR(context.Background(), tasks, taskID, "   ")
	assert.Equal(t, "https://github.com/acme/widget/pull/9", tasks.prs[taskID])
}
