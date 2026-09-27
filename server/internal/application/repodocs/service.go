package repodocs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

type TaskPRMerger interface {
	MergeTaskPullRequest(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPRMergeResult, error)
}

type RepositoryStore interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	Update(ctx context.Context, id uuid.UUID, req domain.UpdateRepositoryRequest) (domain.Repository, error)
	SetDocsTaskID(ctx context.Context, id uuid.UUID, taskID string) error
}

// ComponentStore lets a doc target a project_components row instead of the
// legacy repository/sub-project fields, which projectmodel's projection
// overwrites from components anyway.
type ComponentStore interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	UpdateComponent(ctx context.Context, componentID uuid.UUID, patch domain.ComponentPatch) (domain.Component, error)
}

type Service struct {
	repos      RepositoryStore
	tasks      TaskCreator
	merger     TaskPRMerger
	components ComponentStore

	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }
func (s *Service) SetComponents(c ComponentStore)      { s.components = c }

func NewService(repos RepositoryStore) *Service {
	return &Service{repos: repos}
}

func (s *Service) SetTaskCreator(tasks TaskCreator) { s.tasks = tasks }

func (s *Service) SetTaskPRMerger(m TaskPRMerger) { s.merger = m }

func (s *Service) CreateDocTask(ctx context.Context, repositoryID uuid.UUID, subProjectPath, kind, path string) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	doc, err := s.resolveDoc(ctx, repo, DocItem{Kind: kind, SubProjectPath: subProjectPath, Path: path})
	if err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.setDocPath(ctx, repo, doc); err != nil {
		return domain.BoardTask{}, err
	}

	description := docTaskKey.Render(docTaskInput{Kind: doc.kind, FullPath: doc.fullPath, KindLabel: doc.kindLabel})

	return s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           fmt.Sprintf("Write %s (%s)", doc.fullPath, doc.kindLabel),
		Description:     description,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.systemTaskAssignee(ctx, doc.kindLabel, repo.SubProjects),
	})
}

type DocItem struct {
	Kind           string `json:"kind"`
	ComponentID    string `json:"component_id,omitempty"`
	SubProjectPath string `json:"sub_project_path,omitempty"`
	Path           string `json:"path,omitempty"`
}

type resolvedDoc struct {
	kind           string
	subProjectPath string
	componentID    string
	componentPath  string
	path           string
	fullPath       string
	kindLabel      string
}

// resolveDoc figures out where a requested doc lives. A ComponentID targets a
// project_components row (the current model); everything else falls back to
// the legacy repository/sub-project scoping so the per-kind route keeps
// working unchanged.
func (s *Service) resolveDoc(ctx context.Context, repo domain.Repository, item DocItem) (resolvedDoc, error) {
	kind := strings.TrimSpace(item.Kind)
	if !domain.ValidRepoDocKind(kind) {
		return resolvedDoc{}, fmt.Errorf("invalid doc kind %q", kind)
	}

	componentID := strings.TrimSpace(item.ComponentID)
	if componentID != "" {
		if s.components == nil {
			return resolvedDoc{}, fmt.Errorf("components are not available")
		}
		id, err := uuid.Parse(componentID)
		if err != nil {
			return resolvedDoc{}, fmt.Errorf("invalid component id %q", componentID)
		}
		comp, err := s.components.GetComponent(ctx, id)
		if err != nil {
			return resolvedDoc{}, err
		}
		if comp.RepositoryID != repo.ID {
			return resolvedDoc{}, fmt.Errorf("component %s does not belong to this repository", componentID)
		}
		out := resolvedDoc{
			kind:          kind,
			componentID:   comp.ID.String(),
			componentPath: comp.Path,
			kindLabel:     comp.Role.Get().LegacyRepoKind(),
		}
		dir := ""
		if comp.Path != "." {
			dir = comp.Path + "/"
		}
		out.path = strings.TrimSpace(item.Path)
		if out.path == "" {
			out.path = domain.DefaultRepoDocPath(kind)
		}
		out.fullPath = dir + out.path
		return out, nil
	}

	out := resolvedDoc{kind: kind, kindLabel: repo.Kind, subProjectPath: strings.TrimSpace(item.SubProjectPath)}
	dir := ""
	if out.subProjectPath != "" {
		out.kindLabel = domain.RepoKindBackend
		for _, sp := range repo.SubProjects {
			if sp.Path == out.subProjectPath {
				out.kindLabel = sp.Kind
				break
			}
		}
		dir = out.subProjectPath + "/"
	}
	out.path = strings.TrimSpace(item.Path)
	if out.path == "" {
		out.path = domain.DefaultRepoDocPath(kind)
	}
	out.fullPath = dir + out.path
	return out, nil
}

// NewRepoDocInstructions is the doc_task brief for a repository with no code
// yet: there is nothing to describe, so each doc PRESCRIBES the conventions
// the first commit and every later change must follow.
func NewRepoDocInstructions(kind, fullPath string) string {
	return newRepoDocInstructionsKey.Render(newRepoDocInstructionsInput{Kind: kind, FullPath: fullPath})
}

func (s *Service) CreateDocsBundleTask(ctx context.Context, repositoryID uuid.UUID, items []DocItem) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	if len(items) == 0 {
		return domain.BoardTask{}, fmt.Errorf("no docs requested")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	docs := make([]resolvedDoc, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		doc, err := s.resolveDoc(ctx, repo, item)
		if err != nil {
			return domain.BoardTask{}, err
		}
		scopeKey := doc.subProjectPath
		if doc.componentID != "" {
			scopeKey = doc.componentID
		}
		key := scopeKey + "\x00" + doc.kind
		if seen[key] {
			return domain.BoardTask{}, fmt.Errorf("duplicate doc kind %q for %s", doc.kind, docScopeLabel(doc))
		}
		seen[key] = true
		docs = append(docs, doc)
	}

	for _, doc := range docs {

		current, err := s.repos.Get(ctx, repositoryID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.setDocPath(ctx, current, doc); err != nil {
			return domain.BoardTask{}, err
		}
	}

	task, err := s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           "Generate reference docs",
		Description:     bundleDescription(repo, docs),
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.systemTaskAssigneeForArea(ctx, bundleArea(repo, docs)),
	})
	if err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.repos.SetDocsTaskID(ctx, repositoryID, task.ID.String()); err != nil {
		return domain.BoardTask{}, err
	}
	return task, nil
}

func bundleDescription(repo domain.Repository, docs []resolvedDoc) string {
	bundleDocs := make([]bundleDocInput, len(docs))
	for i, doc := range docs {
		bundleDocs[i] = bundleDocInput{
			Number:     i + 1,
			FullPath:   doc.fullPath,
			KindLabel:  DocKindLabel(doc.kind),
			ScopeLabel: docScopeLabel(doc),
			Kind:       doc.kind,
		}
	}
	return docsBundleKey.Render(docsBundleInput{RepoKind: repo.Kind, Docs: bundleDocs})
}

func DocKindLabel(kind string) string {
	switch kind {
	case domain.RepoDocCodingStandards:
		return "coding standards"
	case domain.RepoDocTestStandards:
		return "test standards"
	case domain.RepoDocArchitecture:
		return "architecture"
	case domain.RepoDocLocalRun:
		return "the local bootstrap script"
	}
	return kind
}

func docScopeLabel(doc resolvedDoc) string {
	if doc.componentID != "" {
		if doc.componentPath == "." {
			return "the repository (" + doc.kindLabel + ")"
		}
		return doc.componentPath + " (" + doc.kindLabel + ")"
	}
	if doc.subProjectPath == "" {
		return "the repository (" + doc.kindLabel + ")"
	}
	return doc.subProjectPath + " (" + doc.kindLabel + ")"
}

type DocsTaskStatus struct {
	TaskID   string `json:"task_id"`
	TaskKey  string `json:"task_key,omitempty"`
	Title    string `json:"title,omitempty"`
	Column   string `json:"column"`
	PRURL    string `json:"pr_url"`
	PRNumber int    `json:"pr_number,omitempty"`
	Merged   bool   `json:"merged"`
}

func (s *Service) DocsTask(ctx context.Context, repositoryID uuid.UUID) (DocsTaskStatus, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return DocsTaskStatus{}, err
	}
	task, ok, err := s.docsTask(ctx, repo)
	if err != nil || !ok {
		return DocsTaskStatus{}, err
	}
	return DocsTaskStatus{
		TaskID:   task.ID.String(),
		TaskKey:  task.Key,
		Title:    task.Title,
		Column:   string(task.Column),
		PRURL:    task.PRURL,
		PRNumber: task.PRNumber,
		Merged:   strings.TrimSpace(task.MergeCommitSHA) != "",
	}, nil
}

func (s *Service) docsTask(ctx context.Context, repo domain.Repository) (domain.BoardTask, bool, error) {
	id := strings.TrimSpace(repo.DocsTaskID)
	if id == "" || s.tasks == nil {
		return domain.BoardTask{}, false, nil
	}
	taskID, err := uuid.Parse(id)
	if err != nil {
		return domain.BoardTask{}, false, nil
	}
	task, err := s.tasks.GetTask(ctx, repo.ID, taskID)
	if err != nil {
		return domain.BoardTask{}, false, nil
	}
	return task, true, nil
}

func (s *Service) MergeDocsTask(ctx context.Context, repositoryID uuid.UUID) (domain.TaskPRMergeResult, error) {
	if s.merger == nil {
		return domain.TaskPRMergeResult{}, fmt.Errorf("this deployment has no GitHub pull-request access wired up")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.TaskPRMergeResult{}, err
	}
	task, ok, err := s.docsTask(ctx, repo)
	if err != nil {
		return domain.TaskPRMergeResult{}, err
	}
	if !ok {
		return domain.TaskPRMergeResult{}, fmt.Errorf("no reference-doc task is outstanding for this repository")
	}
	result, err := s.merger.MergeTaskPullRequest(ctx, repositoryID, task.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrMergeAlreadyMerged) {
			return domain.TaskPRMergeResult{}, err
		}

		result, err = s.alreadyMergedResult(ctx, repositoryID, task)
		if err != nil {
			return domain.TaskPRMergeResult{}, err
		}
	}

	if clearErr := s.repos.SetDocsTaskID(ctx, repositoryID, ""); clearErr != nil {
		result.Message = strings.TrimSpace(result.Message + " WARNING: the docs task could not be cleared from the repository (" + clearErr.Error() + ").")
	}
	return result, nil
}

func (s *Service) alreadyMergedResult(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) (domain.TaskPRMergeResult, error) {
	current, err := s.tasks.GetTask(ctx, repositoryID, task.ID)
	if err != nil {
		current = task
	}
	msg := "Pull request was already merged; nothing further was needed."
	if current.PRNumber > 0 {
		msg = "Pull request #" + strconv.Itoa(current.PRNumber) + " was already merged; nothing further was needed."
	}
	return domain.TaskPRMergeResult{
		Merged:         true,
		PRNumber:       current.PRNumber,
		PRURL:          current.PRURL,
		MergeCommitSHA: current.MergeCommitSHA,
		Message:        msg,
	}, nil
}

func (s *Service) setDocPath(ctx context.Context, repo domain.Repository, doc resolvedDoc) error {
	if doc.componentID != "" {
		id, err := uuid.Parse(doc.componentID)
		if err != nil {
			return fmt.Errorf("invalid component id %q", doc.componentID)
		}
		comp, err := s.components.GetComponent(ctx, id)
		if err != nil {
			return err
		}
		docs := comp.Docs
		docs.SetPath(doc.kind, doc.path)
		_, err = s.components.UpdateComponent(ctx, id, domain.ComponentPatch{Docs: &docs})
		return err
	}
	if doc.subProjectPath == "" {
		docs := repo.Docs
		docs.SetPath(doc.kind, doc.path)
		_, err := s.repos.Update(ctx, repo.ID, domain.UpdateRepositoryRequest{Docs: &docs})
		return err
	}
	subs := make([]domain.RepoSubProject, len(repo.SubProjects))
	copy(subs, repo.SubProjects)
	found := false
	for i := range subs {
		if subs[i].Path == doc.subProjectPath {
			subs[i].Docs.SetPath(doc.kind, doc.path)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("sub-project not found: %s", doc.subProjectPath)
	}
	_, err := s.repos.Update(ctx, repo.ID, domain.UpdateRepositoryRequest{SubProjects: &subs})
	return err
}

func (s *Service) systemTaskAssignee(ctx context.Context, kind string, subProjects []domain.RepoSubProject) *uuid.UUID {
	return s.systemTaskAssigneeForArea(ctx, domain.RepoArea(kind, subProjects))
}

func (s *Service) systemTaskAssigneeForArea(ctx context.Context, area string) *uuid.UUID {
	if s.roles == nil {
		return nil
	}
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, area)
	if err != nil {
		return nil
	}
	return id
}

func bundleArea(repo domain.Repository, docs []resolvedDoc) string {
	owners := make([]domain.RepoSubProject, 0, len(docs))
	for _, doc := range docs {
		if doc.kindLabel == domain.RepoKindMonorepo {
			owners = append(owners, repo.SubProjects...)
			continue
		}
		owners = append(owners, domain.RepoSubProject{Kind: doc.kindLabel})
	}
	return domain.RepoArea(domain.RepoKindMonorepo, owners)
}
