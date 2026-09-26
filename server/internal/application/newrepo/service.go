// Package newrepo creates a brand-new, empty repository from what the person
// says it will be. There is no code to scan yet, so instead of a scan the
// answers become the root component and one bootstrap task that writes the
// skeleton, the reference docs and the agent instructions in a single pull
// request; the first push to the default branch scans it.
package newrepo

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type RepositoryCreator interface {
	CreateWithoutScan(ctx context.Context, req domain.CreateRepositoryRequest) (domain.Repository, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type ComponentWriter interface {
	AddComponent(ctx context.Context, repoID uuid.UUID, req domain.NewComponentRequest) (domain.Component, error)
	UpdateComponent(ctx context.Context, componentID uuid.UUID, patch domain.ComponentPatch) (domain.Component, error)
}

type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
}

type Result struct {
	Repository  domain.Repository
	ComponentID uuid.UUID
	// Task is nil when neither a skeleton nor any doc was asked for.
	Task *domain.BoardTask
}

// IncompleteError is a failure after the repository itself was created: it is
// registered and on GitHub, so the person keeps it and finishes the setup from
// its page rather than retrying the same name.
type IncompleteError struct {
	Repository domain.Repository
	Step       string
	Err        error
}

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("repository %q was created, but %s failed: %v — finish its setup from the repository page", e.Repository.Name, e.Step, e.Err)
}

func (e *IncompleteError) Unwrap() error { return e.Err }

type Service struct {
	repos      RepositoryCreator
	components ComponentWriter
	tasks      TaskCreator
	roles      port.RoleResolver
}

func NewService(repos RepositoryCreator, components ComponentWriter, tasks TaskCreator) *Service {
	return &Service{repos: repos, components: components, tasks: tasks}
}

func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

// Create validates everything before touching disk or GitHub: a request that
// fails validation leaves nothing behind.
func (s *Service) Create(ctx context.Context, req domain.NewRepositoryRequest) (Result, error) {
	role := domain.ComponentRole(strings.TrimSpace(string(req.Role)))
	if role == "" {
		return Result{}, fmt.Errorf("role is required")
	}
	if !domain.ValidComponentRole(role) {
		return Result{}, fmt.Errorf("unknown role %q", role)
	}
	docs, err := normalizeDocs(req.Docs)
	if err != nil {
		return Result{}, err
	}
	if _, err := domain.NewRepoDirName(req.Name); err != nil {
		return Result{}, err
	}
	if s.repos == nil || s.components == nil || s.tasks == nil {
		return Result{}, fmt.Errorf("creating repositories is not available on this server")
	}

	repo, err := s.repos.CreateWithoutScan(ctx, domain.CreateRepositoryRequest{
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		ProjectIDs:  req.ProjectIDs,
		Owner:       strings.TrimSpace(req.Owner),
		Kind:        role.LegacyRepoKind(),
	})
	if err != nil {
		return Result{}, err
	}

	comp, err := s.components.AddComponent(ctx, repo.ID, domain.NewComponentRequest{Path: ".", Name: repo.Name, Role: role})
	if err != nil {
		return Result{}, &IncompleteError{Repository: repo, Step: "adding its root component", Err: err}
	}
	if len(docs) > 0 {
		var paths domain.RepositoryDocs
		for _, kind := range docs {
			paths.SetPath(kind, domain.DefaultRepoDocPath(kind))
		}
		if _, err := s.components.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Docs: &paths}); err != nil {
			return Result{}, &IncompleteError{Repository: repo, Step: "recording its reference docs", Err: err}
		}
	}
	// Adding the component re-projects the legacy repository columns (kind),
	// so the row read back is the one the rest of the app now sees.
	if fresh, err := s.repos.Get(ctx, repo.ID); err == nil {
		repo = fresh
	}

	result := Result{Repository: repo, ComponentID: comp.ID}
	if !req.Scaffold && len(docs) == 0 {
		return result, nil
	}
	componentID := comp.ID
	task, err := s.tasks.CreateTask(ctx, repo.ID, domain.CreateBoardTaskRequest{
		Title:           "Set up " + repo.Name,
		Description:     bootstrapDescription(repo.Name, role, req, docs),
		ComponentID:     &componentID,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.assignee(ctx, role),
	})
	if err != nil {
		return Result{}, &IncompleteError{Repository: repo, Step: "opening its setup task", Err: err}
	}
	result.Task = &task
	return result, nil
}

func normalizeDocs(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		kind := strings.TrimSpace(raw)
		if !domain.ValidRepoDocKind(kind) {
			return nil, fmt.Errorf("unknown doc kind %q", raw)
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out, nil
}

func (s *Service) assignee(ctx context.Context, role domain.ComponentRole) *uuid.UUID {
	if s.roles == nil {
		return nil
	}
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, domain.RepoArea(role.LegacyRepoKind(), nil))
	if err != nil {
		return nil
	}
	return id
}

func bootstrapDescription(name string, role domain.ComponentRole, req domain.NewRepositoryRequest, docs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Set up the brand-new repository %s. It was just created empty — an initial commit and nothing else — so there is no existing code to read or follow: what this task writes sets the conventions for everything after it.\n\n", name)

	b.WriteString("## What the person asked for\n\n")
	b.WriteString("Their answers, verbatim:\n\n")
	writeAnswer(&b, "Description", req.Description)
	writeAnswer(&b, "Role", string(role))
	writeAnswer(&b, "Stack", req.Stack)
	writeAnswer(&b, "Notes", req.Notes)
	if strings.TrimSpace(req.Stack) == "" {
		fmt.Fprintf(&b, "No stack was named: choose a mainstream, well-supported stack for a %s project that fits the description, and state the choice and the reason in the README.\n\n", role)
	}

	b.WriteString("## What to deliver\n\n")
	b.WriteString("Do ALL of it on a single branch, in exactly one pull request. Do not open a pull request per item and do not stop after the first one — the task is finished when every item below exists and is correct.\n\n")
	n := 0
	item := func(text string) {
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, text)
	}
	if req.Scaffold {
		item("the initial project skeleton in the repository root")
	}
	for _, kind := range docs {
		item(fmt.Sprintf("`%s` — %s", domain.DefaultRepoDocPath(kind), repodocs.DocKindLabel(kind)))
	}
	item("`CLAUDE.md` and `AGENTS.md` at the repository root")

	if req.Scaffold {
		b.WriteString("\n---\n\n## The project skeleton\n\n")
		b.WriteString("Create the initial project skeleton for this stack in the repository root: minimal but runnable — it installs, builds and starts (a library: builds and runs its tests) with the stack's standard commands — plus a README that says what the project is and how to install, run and test it. Nothing beyond what proves it runs.\n")
		b.WriteString("Before writing it by hand, call search_boilerplate_catalog with the stack and the role; when a starter there fits, build on it instead of starting from scratch, and say in the pull request which one you used.\n")
		if len(docs) > 0 {
			b.WriteString("The skeleton must already follow every convention the documents below prescribe.\n")
		}
	}
	for _, kind := range docs {
		path := domain.DefaultRepoDocPath(kind)
		fmt.Fprintf(&b, "\n---\n\n## `%s`\n\n", path)
		b.WriteString(repodocs.NewRepoDocInstructions(kind, path))
	}

	b.WriteString("\n---\n\n## `CLAUDE.md` and `AGENTS.md`\n\n")
	b.WriteString("Create both at the repository root, or refresh them if the skeleton already produced one: a one-paragraph summary of what this project is, then a short docs index linking every document written in this pull request")
	if req.Scaffold {
		b.WriteString(" and the README")
	}
	b.WriteString(", so every agent that opens this repository finds them. Keep the two files' content the same.\n")
	return b.String()
}

func writeAnswer(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "(not given)"
	}
	fmt.Fprintf(b, "**%s**\n%s\n\n", label, value)
}
