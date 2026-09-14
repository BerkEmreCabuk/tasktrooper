package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// resolveRepositoryRef turns a repository reference into an id. The reference is
// either a UUID or a repository name — the same name-or-UUID contract the
// assignee argument already uses. Requiring a raw UUID meant an agent had to
// call list_repositories and copy an id before it could tag a task, so in
// practice it skipped the field and every task landed on the default repo.
func (kit *ToolKit) resolveRepositoryRef(ctx context.Context, ref string) (uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return uuid.Nil, fmt.Errorf("empty repository reference")
	}
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	if kit.Workspace == nil {
		return uuid.Nil, fmt.Errorf("repository %q is not a UUID and the repository list is unavailable to resolve it", ref)
	}
	repos, err := kit.Workspace.ListRepositories(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve repository: %v", err)
	}
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		if strings.EqualFold(r.Name, ref) {
			return r.ID, nil
		}
		names = append(names, r.Name)
	}
	if len(names) == 0 {
		return uuid.Nil, fmt.Errorf("unknown repository %q; no repositories are registered", ref)
	}
	return uuid.Nil, fmt.Errorf("unknown repository %q; registered repositories: %s", ref, strings.Join(names, ", "))
}

// resolveTaskRef turns a task reference into an id. The reference is either a
// UUID or the board key the whole product shows the user and the agent — "T-1"
// for work, "B-1" for a bug, "A-1" for an analysis.
//
// UUID-only meant every reference the model picked up from the chat, the board,
// or the session action ledger's key field came back as "invalid task_id". A
// move that fails that way does not stay failed: the model concludes the task is
// unreachable and opens a new one instead, which is exactly the duplicate this
// resolver removes the reason for.
func (kit *ToolKit) resolveTaskRef(ctx context.Context, ref string) (uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return uuid.Nil, fmt.Errorf("empty task reference")
	}
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	tasks, err := kit.listBoardTasks(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve task %q: %v", ref, err)
	}
	for _, t := range tasks {
		if strings.EqualFold(t.Key, ref) {
			return t.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("unknown task %q; pass the task UUID or its board key (e.g. T-1, B-1, A-1) — list_board_tasks shows both", ref)
}

// resolveDeployDependencies turns a list of task references (UUIDs or board
// keys) into deploy_depends_on relation inputs. Resolving here rather than in
// the service means an unknown key fails with resolveTaskRef's message — which
// names both accepted spellings and points at list_board_tasks — instead of a
// bare not-found the model cannot act on.
func (kit *ToolKit) resolveDeployDependencies(ctx context.Context, refs []string) ([]domain.TaskRelationInput, error) {
	return kit.resolveRelationRefs(ctx, refs, domain.TaskRelationDeployDependsOn, "deploy_depends_on")
}

// resolveRelationRefs is the general form: turn a list of task references into
// relation inputs of one type, naming the ARGUMENT in any error.
//
// The argument name matters more than it looks. All three ordering arguments
// take the same shape and fail the same way, and a model told only "unknown task
// T-9" has to guess which of its lists was wrong before it can retry.
func (kit *ToolKit) resolveRelationRefs(ctx context.Context, refs []string, relType domain.TaskRelationType, argName string) ([]domain.TaskRelationInput, error) {
	out := make([]domain.TaskRelationInput, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		targetID, err := kit.resolveTaskRef(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", argName, err)
		}
		out = append(out, domain.TaskRelationInput{
			TargetTaskID: targetID,
			RelationType: relType,
		})
	}
	return out, nil
}

// resolveProjectRef turns an initiative project reference (UUID or name) into an
// id, mirroring resolveRepositoryRef.
func (kit *ToolKit) resolveProjectRef(ctx context.Context, ref string) (uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return uuid.Nil, fmt.Errorf("empty project reference")
	}
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	if kit.Workspace == nil {
		return uuid.Nil, fmt.Errorf("project %q is not a UUID and the project list is unavailable to resolve it", ref)
	}
	projects, err := kit.Workspace.ListProjects(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve project: %v", err)
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		if strings.EqualFold(p.Name, ref) {
			return p.ID, nil
		}
		names = append(names, p.Name)
	}
	if len(names) == 0 {
		return uuid.Nil, fmt.Errorf("unknown project %q; no projects exist yet — create one with create_project", ref)
	}
	return uuid.Nil, fmt.Errorf("unknown project %q; existing projects: %s", ref, strings.Join(names, ", "))
}
