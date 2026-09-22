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

// taskChatWorkspace prepares the git checkout a task-bound chat runs in — the
// session service's session.TaskWorkspaceResolver. It lives here as pure
// wiring: session must not know about board stores or git, board must not know
// about sessions, and the two facts meet once.
//
// A TASK-bound chat gets the task's own checkout on the task's own branch —
// the SAME checkout the board runner uses, so a chat and a run edit the same
// tree. A repository-scoped chat points at the SHARED mirror clone, which
// git.SyncDefaultBranch resets onto origin's default branch, so an edit there
// is on the wrong branch and would be discarded by the next index pass.
type taskChatWorkspace struct {
	tasks         port.BoardTaskStore
	criteria      port.AcceptanceCriterionStore
	repos         session.RepositoryResolver
	git           port.GitClient
	workspaceRoot string
}

func (w taskChatWorkspace) ResolveTaskWorkspace(ctx context.Context, repositoryID, taskID uuid.UUID) (session.TaskBinding, error) {
	// A task id from another repository comes back not-found rather than
	// handing this chat that repository's checkout.
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
	// A non-git project has no branch to check out: the chat runs in the
	// repository root with Branch empty (nothing can be committed/pushed there,
	// and commit_task_changes says so).
	if w.git == nil || w.workspaceRoot == "" || !w.git.HasGit(root) {
		binding.WorkspaceDir = root
		return binding, nil
	}
	branch := domain.TaskBranchName(task)
	workspacePath, err := workspace.TaskDir(w.workspaceRoot, taskID)
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
