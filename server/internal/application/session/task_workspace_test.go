package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// workspaceStore records where the session's workspace_dir was moved to, which is
// the persisted half of the decision under test.
type workspaceStore struct {
	stubSessionStore
	movedTo string
}

func (s *workspaceStore) UpdateWorkspaceDir(_ context.Context, _ uuid.UUID, dir string) error {
	s.movedTo = dir
	return nil
}

// mirrorRepos stands in for the repository resolver: it only knows the SHARED
// mirror clone, which is exactly the wrong tree for a task-bound chat.
type mirrorRepos struct{ root string }

func (r mirrorRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return r.root, nil
}
func (r mirrorRepos) ResolveDescription(context.Context, uuid.UUID) (string, error) { return "", nil }
func (r mirrorRepos) ResolveRepository(context.Context, uuid.UUID) (domain.Repository, error) {
	return domain.Repository{}, errors.New("not needed")
}

type stubTaskWorkspaces struct {
	binding      session.TaskBinding
	err          error
	gotRepoID    uuid.UUID
	gotTaskID    uuid.UUID
	resolveCalls int
}

func (s *stubTaskWorkspaces) ResolveTaskWorkspace(_ context.Context, repositoryID, taskID uuid.UUID) (session.TaskBinding, error) {
	s.gotRepoID, s.gotTaskID = repositoryID, taskID
	s.resolveCalls++
	return s.binding, s.err
}

// The whole feature rests on this: a chat about a task must run in that task's own
// branch checkout. The mirror clone is force-reset onto origin's default branch by
// SyncDefaultBranch, so an edit made there is on the wrong branch and is thrown
// away — it could never reach the task's pull request.
func TestTaskBoundSessionRunsInTheTaskWorkspaceNotTheMirror(t *testing.T) {
	repositoryID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{
		ID: taskID, RepositoryID: repositoryID, Key: "DE-1", Title: "Add the store link",
		Column: domain.TaskColumnInProgress, PRURL: "https://github.com/acme/widget/pull/42", PRNumber: 42,
	}
	tasks := &stubTaskWorkspaces{binding: session.TaskBinding{
		Task:         task,
		Criteria:     []domain.AcceptanceCriterion{{Text: "the footer links to the store", Completed: false}},
		WorkspaceDir: "/data/workspaces/task-" + taskID.String(),
		Branch:       "feature/task-1234abcd-add-the-store-link",
	}}
	store := &workspaceStore{}
	sess := domain.Session{
		ID: uuid.New(), ProjectID: &repositoryID, TaskID: &taskID,
		WorkspaceDir: "/repos/widget", // the mirror clone recorded at creation
	}

	dir, runCtx, history, err := session.ResolveRunWorkspaceForTest(
		context.Background(), store, mirrorRepos{root: "/repos/widget"}, tasks, sess,
		domain.AppSettings{WorkspaceRoot: "/data/workspaces", DefaultLanguage: "en"},
		[]domain.Message{{Role: domain.RoleUser, Content: "why does the footer link 404?"}},
	)
	require.NoError(t, err)

	assert.Equal(t, "/data/workspaces/task-"+taskID.String(), dir)
	assert.NotEqual(t, "/repos/widget", dir, "a task chat must never work in the shared mirror clone")
	assert.Equal(t, repositoryID, tasks.gotRepoID)
	assert.Equal(t, taskID, tasks.gotTaskID)
	// The corrected path is persisted, so the next turn (and the UI) agree with it.
	assert.Equal(t, "/data/workspaces/task-"+taskID.String(), store.movedTo)

	// The context carries what the PR tools resolve the task from, and what
	// retrieval prefers the branch's index by.
	assert.Equal(t, taskID, registry.TaskIDFromContext(runCtx))
	assert.Equal(t, "feature/task-1234abcd-add-the-store-link", registry.BranchFromContext(runCtx))
	assert.Equal(t, "/data/workspaces/task-"+taskID.String(), registry.EffectiveWorkspaceDir(runCtx))

	// The task context leads the prompt, names the PR, and points at the tools —
	// but does not carry the diff, which is paid for per turn and belongs in a
	// tool result.
	require.NotEmpty(t, history)
	first := history[0]
	assert.Equal(t, domain.RoleSystem, first.Role)
	assert.Contains(t, first.Content, "DE-1")
	assert.Contains(t, first.Content, "feature/task-1234abcd-add-the-store-link")
	assert.Contains(t, first.Content, "#42")
	assert.Contains(t, first.Content, "get_task_pull_request")
	assert.Contains(t, first.Content, "commit_task_changes")
	assert.NotContains(t, first.Content, "diff --git")
}

// A workspace that cannot be prepared fails the turn instead of quietly falling
// back to the mirror — the same hard gate the board runner applies, and for the
// same reason: running on the wrong tree risks pushing to the default branch.
func TestTaskBoundSessionFailsRatherThanFallBackToTheMirror(t *testing.T) {
	repositoryID, taskID := uuid.New(), uuid.New()
	tasks := &stubTaskWorkspaces{err: errors.New("branch checkout conflicts with origin")}
	store := &workspaceStore{}
	sess := domain.Session{ID: uuid.New(), ProjectID: &repositoryID, TaskID: &taskID, WorkspaceDir: "/repos/widget"}

	_, _, _, err := session.ResolveRunWorkspaceForTest(
		context.Background(), store, mirrorRepos{root: "/repos/widget"}, tasks, sess,
		domain.AppSettings{WorkspaceRoot: "/data/workspaces"}, nil,
	)

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "task's working copy"), "the error must say what could not be prepared: %v", err)
	assert.Empty(t, store.movedTo, "a failed checkout must not repoint the session")
}

// A chat with no task keeps the old behaviour exactly: repository root, no task
// context message, no branch pinned.
func TestSessionWithoutATaskStillUsesTheRepositoryRoot(t *testing.T) {
	repositoryID := uuid.New()
	tasks := &stubTaskWorkspaces{}
	store := &workspaceStore{}
	sess := domain.Session{ID: uuid.New(), ProjectID: &repositoryID}

	dir, runCtx, history, err := session.ResolveRunWorkspaceForTest(
		context.Background(), store, mirrorRepos{root: t.TempDir()}, tasks, sess,
		domain.AppSettings{WorkspaceRoot: t.TempDir(), DefaultLanguage: "en"},
		[]domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	)
	require.NoError(t, err)

	assert.NotEmpty(t, dir)
	assert.Zero(t, tasks.resolveCalls, "no task, no task workspace resolution")
	assert.Equal(t, uuid.Nil, registry.TaskIDFromContext(runCtx))
	assert.Empty(t, registry.BranchFromContext(runCtx))
	for _, m := range history {
		assert.NotContains(t, m.Content, "The board task this conversation is about")
	}
}
