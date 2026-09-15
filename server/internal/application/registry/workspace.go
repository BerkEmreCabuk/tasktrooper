package registry

import (
	"context"

	"github.com/google/uuid"
)

func ContextWithWorkspaceDir(ctx context.Context, dir string) context.Context {
	if dir == "" {
		return ctx
	}
	return context.WithValue(ctx, workspaceDirKey, dir)
}

func ContextWithSubtaskWorkspace(ctx context.Context, dir string) context.Context {
	if dir == "" {
		return ctx
	}
	return context.WithValue(ctx, subtaskWorkspaceKey, dir)
}

func WorkspaceDirFromContext(ctx context.Context) string {
	if v := ctx.Value(workspaceDirKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func SubtaskWorkspaceFromContext(ctx context.Context) string {
	if v := ctx.Value(subtaskWorkspaceKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func EffectiveWorkspaceDir(ctx context.Context) string {
	if dir := SubtaskWorkspaceFromContext(ctx); dir != "" {
		return dir
	}
	return WorkspaceDirFromContext(ctx)
}

const taskEnvKey contextKey = "task_env"
const branchKey contextKey = "task_branch"

// ContextWithBranch records which git branch the run's workspace has checked
// out, so retrieval can prefer that branch's index.
func ContextWithBranch(ctx context.Context, branch string) context.Context {
	if branch == "" {
		return ctx
	}
	return context.WithValue(ctx, branchKey, branch)
}

func BranchFromContext(ctx context.Context) string {
	if v := ctx.Value(branchKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ContextWithTaskEnv carries the per-task environment overlay (resolved
// toolchain versions, PATH) down to tools that spawn processes in the task's
// workspace. Entries are KEY=VALUE pairs appended after os.Environ().
func ContextWithTaskEnv(ctx context.Context, env []string) context.Context {
	if len(env) == 0 {
		return ctx
	}
	return context.WithValue(ctx, taskEnvKey, env)
}

func TaskEnvFromContext(ctx context.Context) []string {
	if v := ctx.Value(taskEnvKey); v != nil {
		if env, ok := v.([]string); ok {
			return env
		}
	}
	return nil
}

func ContextWithSessionID(ctx context.Context, id uuid.UUID) context.Context {
	if id == uuid.Nil {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey, id)
}

func SessionIDFromContext(ctx context.Context) uuid.UUID {
	if v := ctx.Value(sessionIDKey); v != nil {
		if id, ok := v.(uuid.UUID); ok {
			return id
		}
	}
	return uuid.Nil
}

func ContextWithRepositoryID(ctx context.Context, id uuid.UUID) context.Context {
	if id == uuid.Nil {
		return ctx
	}
	return context.WithValue(ctx, repositoryIDKey, id)
}

func RepositoryIDFromContext(ctx context.Context) uuid.UUID {
	if v := ctx.Value(repositoryIDKey); v != nil {
		if id, ok := v.(uuid.UUID); ok {
			return id
		}
	}
	return uuid.Nil
}

func ContextWithProjectID(ctx context.Context, id uuid.UUID) context.Context {
	return ContextWithRepositoryID(ctx, id)
}

func ProjectIDFromContext(ctx context.Context) uuid.UUID {
	return RepositoryIDFromContext(ctx)
}

// ContextWithTaskID names the board task a run is working, so a tool called
// without an explicit task_id can still act on the right one.
//
// It exists for the task-scoped chat: the human says "fix the null check" and the
// agent has to know which task's branch and pull request that means. Making the
// tool argument the only source would put the burden on the model to carry the id
// through every turn of a conversation whose whole subject is one task — and a
// model that guesses wrong commits to another task's branch.
func ContextWithTaskID(ctx context.Context, id uuid.UUID) context.Context {
	if id == uuid.Nil {
		return ctx
	}
	return context.WithValue(ctx, taskIDKey, id)
}

func TaskIDFromContext(ctx context.Context) uuid.UUID {
	if v := ctx.Value(taskIDKey); v != nil {
		if id, ok := v.(uuid.UUID); ok {
			return id
		}
	}
	return uuid.Nil
}

func ContextWithAgentID(ctx context.Context, id uuid.UUID) context.Context {
	if id == uuid.Nil {
		return ctx
	}
	return context.WithValue(ctx, agentIDKey, id)
}

func AgentIDFromContext(ctx context.Context) uuid.UUID {
	if v := ctx.Value(agentIDKey); v != nil {
		if id, ok := v.(uuid.UUID); ok {
			return id
		}
	}
	return uuid.Nil
}
