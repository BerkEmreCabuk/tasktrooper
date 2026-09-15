package runtime

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// taskChatWorkspace prepares the git checkout a task-bound chat runs in, and is
// the session service's session.TaskWorkspaceResolver.
//
// It exists in this package because it is pure wiring: the session package must
// not know about board task stores or git, and the board package must not know
// about sessions. Both facts meet here, once.
//
// What it decides is which tree the agent edits. A repository-scoped chat points
// at the SHARED mirror clone, which git.SyncDefaultBranch force-resets onto
// origin's default branch — an edit made there is on the wrong branch and is
// discarded by the next index pass, so it could never reach the task's pull
// request. A task-bound chat gets the task's own checkout on the task's own
// branch instead — the SAME checkout the board runner and the pipeline use for
// that task, so a chat and a run are working the same tree.
type taskChatWorkspace struct {
	tasks         port.BoardTaskStore
	criteria      port.AcceptanceCriterionStore
	repos         session.RepositoryResolver
	git           port.GitClient
	workspaceRoot string
}

func (w taskChatWorkspace) ResolveTaskWorkspace(ctx context.Context, repositoryID, taskID uuid.UUID) (session.TaskBinding, error) {
	// Repository-scoped read: a task id from another repository comes back
	// not-found rather than handing this chat that repository's checkout.
	task, err := w.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return session.TaskBinding{}, err
	}
	binding := session.TaskBinding{Task: task}
	if w.criteria != nil {
		// Best-effort: the criteria enrich the prompt, they do not gate the turn.
		if items, listErr := w.criteria.ListByTask(ctx, taskID); listErr == nil {
			binding.Criteria = items
		}
	}

	root, err := w.repos.ResolveRootPath(ctx, repositoryID)
	if err != nil {
		return session.TaskBinding{}, err
	}
	// Same guard the runner applies before it insists on a task workspace: a
	// project that is not a git working copy has no branch to check out, so the
	// chat runs in the repository root and Branch stays empty (nothing can be
	// committed or pushed there, and commit_task_changes says so).
	if w.git == nil || w.workspaceRoot == "" || !w.git.HasGit(root) {
		binding.WorkspaceDir = root
		return binding, nil
	}
	branch := domain.TaskBranchName(task)
	workspacePath, err := workspace.TenantTaskDir(ctx, w.workspaceRoot, taskID)
	if err != nil {
		return session.TaskBinding{}, err
	}
	if err := w.git.EnsureTaskWorkspace(ctx, root, workspacePath, branch); err != nil {
		return session.TaskBinding{}, fmt.Errorf("check out branch %s for task %s: %w", branch, task.Key, err)
	}
	binding.WorkspaceDir = workspacePath
	binding.Branch = branch
	return binding, nil
}
