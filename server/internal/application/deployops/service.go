package deployops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	runs       port.DeploymentRunStore
	dispatches port.DeployDispatchStore
	audit      port.OpsAuditStore
	targets    port.DeployTargetStore
	repos      port.RepositoryStore
	pipeline   port.RepositoryPipelineJobStore
	actions    port.ActionsClient

	now func() time.Time

	repoResolver func(ctx context.Context, repo domain.Repository) (owner, name string, err error)
}

func New(
	runs port.DeploymentRunStore,
	dispatches port.DeployDispatchStore,
	audit port.OpsAuditStore,
	targets port.DeployTargetStore,
	repos port.RepositoryStore,
	pipeline port.RepositoryPipelineJobStore,
	actions port.ActionsClient,
) *Service {
	return &Service{
		runs:       runs,
		dispatches: dispatches,
		audit:      audit,
		targets:    targets,
		repos:      repos,
		pipeline:   pipeline,
		actions:    actions,
		now:        time.Now,
	}
}

func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) SetRepoResolver(fn func(ctx context.Context, repo domain.Repository) (owner, name string, err error)) {
	s.repoResolver = fn
}

type MatrixCell struct {
	Env          string                `json:"env"`
	Configured   bool                  `json:"configured"`
	Dispatchable bool                  `json:"dispatchable"`
	Provider     string                `json:"provider"`
	HealthURL    string                `json:"health_url"`
	AutoRollback bool                  `json:"auto_rollback"`
	LastRun      *domain.DeploymentRun `json:"last_run,omitempty"`

	RollbackSHA string `json:"rollback_sha"`
}

type MatrixRepo struct {
	ID    uuid.UUID    `json:"id"`
	Name  string       `json:"name"`
	Kind  string       `json:"kind"`
	Cells []MatrixCell `json:"cells"`
}

type MatrixView struct {
	Repos []MatrixRepo `json:"repos"`
	Envs  []string     `json:"envs"`
}

func (s *Service) Matrix(ctx context.Context) (MatrixView, error) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		return MatrixView{}, fmt.Errorf("deployops: listing repositories: %w", err)
	}
	targets, err := s.targets.ListAll(ctx)
	if err != nil {
		return MatrixView{}, fmt.Errorf("deployops: listing deploy targets: %w", err)
	}
	jobs, err := s.pipeline.ListAll(ctx)
	if err != nil {
		return MatrixView{}, fmt.Errorf("deployops: listing pipeline jobs: %w", err)
	}
	latest, err := s.runs.LatestAll(ctx)
	if err != nil {
		return MatrixView{}, fmt.Errorf("deployops: listing latest runs: %w", err)
	}

	targetsByRepo := make(map[uuid.UUID]map[string]domain.DeployTarget)
	for _, t := range targets {
		if targetsByRepo[t.RepositoryID] == nil {
			targetsByRepo[t.RepositoryID] = map[string]domain.DeployTarget{}
		}
		targetsByRepo[t.RepositoryID][t.Env] = t
	}

	dispatchableByRepo := make(map[uuid.UUID]map[string]bool)
	for _, j := range jobs {
		if j.TargetKind != domain.PipelineTargetWorkflow || j.TargetRef == "" {
			continue
		}
		env := envForPipelineCategory(j.Category)
		if env == "" {
			continue
		}
		if dispatchableByRepo[j.RepositoryID] == nil {
			dispatchableByRepo[j.RepositoryID] = map[string]bool{}
		}
		dispatchableByRepo[j.RepositoryID][env] = true
	}

	latestByRepo := make(map[uuid.UUID]map[string]domain.DeploymentRun)
	for _, r := range latest {
		if latestByRepo[r.RepositoryID] == nil {
			latestByRepo[r.RepositoryID] = map[string]domain.DeploymentRun{}
		}
		latestByRepo[r.RepositoryID][r.Env] = r
	}

	envs := domain.DeployEnvs()
	view := MatrixView{Envs: envs}
	for _, repo := range repos {
		repoTargets := targetsByRepo[repo.ID]
		if len(repoTargets) == 0 {

			continue
		}
		mr := MatrixRepo{ID: repo.ID, Name: repo.Name, Kind: repo.Kind, Cells: make([]MatrixCell, 0, len(envs))}
		for _, env := range envs {
			cell := MatrixCell{Env: env}
			if target, ok := repoTargets[env]; ok {
				cell.Configured = true
				cell.Provider = target.Provider
				cell.HealthURL = target.HealthURL
				cell.AutoRollback = target.AutoRollback
			}
			cell.Dispatchable = cell.Configured && dispatchableByRepo[repo.ID][env]

			if run, ok := latestByRepo[repo.ID][env]; ok {
				runCopy := run
				cell.LastRun = &runCopy

				rollback, rerr := s.runs.LastSuccessfulBefore(ctx, repo.ID, env, run.HeadSHA)
				switch {
				case rerr == nil:
					cell.RollbackSHA = rollback.HeadSHA
				case errors.Is(rerr, port.ErrNotFound):

					cell.RollbackSHA = ""
				default:
					return MatrixView{}, fmt.Errorf("deployops: rollback lookup for %s %s: %w", repo.ID, env, rerr)
				}
			}
			mr.Cells = append(mr.Cells, cell)
		}
		view.Repos = append(view.Repos, mr)
	}
	return view, nil
}

func envForPipelineCategory(category string) string {
	for _, env := range domain.DeployEnvs() {
		if domain.DeployEnvCategory(env) == category {
			return env
		}
	}
	return ""
}

func (s *Service) Runs(ctx context.Context, repositoryID uuid.UUID, env string, limit int) ([]domain.DeploymentRun, error) {
	limit = clampLimit(limit, 20, 100)
	runs, err := s.runs.ListByEnv(ctx, repositoryID, env, limit)
	if err != nil {
		return nil, fmt.Errorf("deployops: listing runs: %w", err)
	}
	return runs, nil
}

func (s *Service) Audit(ctx context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error) {
	limit = clampLimit(limit, 50, 200)
	entries, err := s.audit.List(ctx, repositoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("deployops: listing audit entries: %w", err)
	}
	return entries, nil
}

func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		limit = def
	}
	if limit > max {
		limit = max
	}
	return limit
}

const defaultDispatchRef = "main"

var (
	ErrNoWorkflowMapping = errors.New("no deploy workflow mapped for this environment")

	ErrNoRollbackTarget = errors.New("no previous successful deploy to roll back to")

	ErrConfirmMismatch = errors.New("confirmation phrase does not match the repository name")

	ErrProvider = errors.New("github request failed")

	ErrNoRepoResolver = errors.New("no repository resolver configured")
)

type DispatchInput struct {
	RepositoryID uuid.UUID
	Env          string
	Ref          string
	Confirm      string
	Actor        string
}

type RollbackInput struct {
	RepositoryID uuid.UUID
	Env          string
	Confirm      string
	Actor        string
}

func productionClass(env string, kind string) bool {
	return kind == domain.DispatchKindRollback || env == domain.DeployEnvProd
}

func (s *Service) resolveDeployWorkflow(ctx context.Context, repositoryID uuid.UUID, env string) (string, error) {
	jobs, err := s.pipeline.ListByRepository(ctx, repositoryID)
	if err != nil {
		return "", fmt.Errorf("deployops: listing pipeline jobs: %w", err)
	}
	category := domain.DeployEnvCategory(env)
	for _, j := range jobs {
		if j.Category == category && j.TargetKind == domain.PipelineTargetWorkflow && j.TargetRef != "" {
			return j.TargetRef, nil
		}
	}
	return "", nil
}

func (s *Service) resolveRepoCoordinates(ctx context.Context, repo domain.Repository) (owner, name string, err error) {
	if s.repoResolver == nil {
		return "", "", ErrNoRepoResolver
	}
	owner, name, err = s.repoResolver(ctx, repo)
	if err != nil {
		return "", "", fmt.Errorf("deployops: resolving repository coordinates: %w", err)
	}
	return owner, name, nil
}

func (s *Service) recordAudit(ctx context.Context, repositoryID uuid.UUID, action, target, actor string, detail map[string]string, actionErr error) {
	entry := domain.OpsAuditEntry{
		RepositoryID: &repositoryID,
		Action:       action,
		Target:       target,
		Actor:        actor,
		Detail:       detail,
		Outcome:      domain.OpsOutcomeOK,
	}
	if actionErr != nil {
		entry.Outcome = domain.OpsOutcomeError
		entry.Error = actionErr.Error()
	}
	if err := s.audit.Log(ctx, entry); err != nil {
		log.Error().Err(err).Str("action", action).Str("target", target).
			Msg("deployops: writing ops audit entry failed")
	}
}

func (s *Service) Dispatch(ctx context.Context, in DispatchInput) (domain.DeployDispatch, error) {
	repo, err := s.repos.Get(ctx, in.RepositoryID)
	if err != nil {

		return domain.DeployDispatch{}, fmt.Errorf("deployops: loading repository: %w", err)
	}

	if productionClass(in.Env, domain.DispatchKindDeploy) && strings.TrimSpace(in.Confirm) != repo.Name {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, in.Env, in.Actor, nil, ErrConfirmMismatch)
		return domain.DeployDispatch{}, ErrConfirmMismatch
	}

	workflowFile, err := s.resolveDeployWorkflow(ctx, in.RepositoryID, in.Env)
	if err != nil {
		return domain.DeployDispatch{}, err
	}
	if workflowFile == "" {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, in.Env, in.Actor, nil, ErrNoWorkflowMapping)
		return domain.DeployDispatch{}, ErrNoWorkflowMapping
	}

	owner, name, err := s.resolveRepoCoordinates(ctx, repo)
	if err != nil {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, in.Env, in.Actor, nil, err)
		return domain.DeployDispatch{}, err
	}

	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		ref = defaultDispatchRef
	}

	dispatch, err := s.dispatches.Create(ctx, domain.DeployDispatch{
		RepositoryID: in.RepositoryID,
		Env:          in.Env,
		WorkflowFile: workflowFile,
		Ref:          ref,
		Kind:         domain.DispatchKindDeploy,
		Actor:        in.Actor,
		State:        domain.DispatchStatePending,
	})
	if err != nil {
		return domain.DeployDispatch{}, fmt.Errorf("deployops: recording dispatch: %w", err)
	}

	if err := s.actions.DispatchWorkflow(ctx, owner, name, workflowFile, ref); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrProvider, err)
		_ = s.dispatches.Resolve(ctx, dispatch.ID, domain.DispatchStateAbandoned, nil)
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, in.Env, in.Actor,
			map[string]string{"ref": ref, "workflow_file": workflowFile}, wrapped)
		return domain.DeployDispatch{}, wrapped
	}

	s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, in.Env, in.Actor,
		map[string]string{"ref": ref, "workflow_file": workflowFile}, nil)
	return dispatch, nil
}

func (s *Service) Rollback(ctx context.Context, in RollbackInput) (domain.DeployDispatch, error) {
	repo, err := s.repos.Get(ctx, in.RepositoryID)
	if err != nil {
		return domain.DeployDispatch{}, fmt.Errorf("deployops: loading repository: %w", err)
	}

	if productionClass(in.Env, domain.DispatchKindRollback) && strings.TrimSpace(in.Confirm) != repo.Name {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor, nil, ErrConfirmMismatch)
		return domain.DeployDispatch{}, ErrConfirmMismatch
	}
	return s.rollback(ctx, in.RepositoryID, in.Env, in.Actor, repo, nil)
}

type AgentRollbackAuthorization struct {
	TaskID  uuid.UUID
	TaskKey string

	OwnedMergeSHA string

	Trigger string

	AgentName string
}

func (s *Service) RollbackForTask(ctx context.Context, in RollbackInput, auth AgentRollbackAuthorization) (domain.DeployDispatch, error) {
	repo, err := s.repos.Get(ctx, in.RepositoryID)
	if err != nil {
		return domain.DeployDispatch{}, fmt.Errorf("deployops: loading repository: %w", err)
	}
	if strings.TrimSpace(auth.OwnedMergeSHA) == "" || auth.TaskID == uuid.Nil {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor, nil, domain.ErrRollbackNotOwner)
		return domain.DeployDispatch{}, domain.ErrRollbackNotOwner
	}
	return s.rollback(ctx, in.RepositoryID, in.Env, in.Actor, repo, &auth)
}

func (s *Service) rollback(ctx context.Context, repositoryID uuid.UUID, env, actor string, repo domain.Repository, auth *AgentRollbackAuthorization) (domain.DeployDispatch, error) {
	in := RollbackInput{RepositoryID: repositoryID, Env: env, Actor: actor}
	workflowFile, err := s.resolveDeployWorkflow(ctx, in.RepositoryID, in.Env)
	if err != nil {
		return domain.DeployDispatch{}, err
	}
	if workflowFile == "" {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor, rollbackAuditDetail(nil, auth), ErrNoWorkflowMapping)
		return domain.DeployDispatch{}, ErrNoWorkflowMapping
	}

	currentSHA := ""
	if current, err := s.runs.Latest(ctx, in.RepositoryID, in.Env); err == nil {
		currentSHA = current.HeadSHA
	} else if !errors.Is(err, port.ErrNotFound) {
		return domain.DeployDispatch{}, fmt.Errorf("deployops: loading current run: %w", err)
	}

	target, err := s.runs.LastSuccessfulBefore(ctx, in.RepositoryID, in.Env, currentSHA)
	if errors.Is(err, port.ErrNotFound) {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor, rollbackAuditDetail(nil, auth), ErrNoRollbackTarget)
		return domain.DeployDispatch{}, ErrNoRollbackTarget
	}
	if err != nil {
		return domain.DeployDispatch{}, fmt.Errorf("deployops: finding rollback target: %w", err)
	}

	owner, name, err := s.resolveRepoCoordinates(ctx, repo)
	if err != nil {
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor, rollbackAuditDetail(nil, auth), err)
		return domain.DeployDispatch{}, err
	}

	tag := fmt.Sprintf("rollback/%s/%d", in.Env, s.now().Unix())
	if err := s.actions.CreateTag(ctx, owner, name, tag, target.HeadSHA); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrProvider, err)
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor,
			map[string]string{"tag": tag, "sha": target.HeadSHA}, wrapped)
		return domain.DeployDispatch{}, wrapped
	}

	dispatch, err := s.dispatches.Create(ctx, domain.DeployDispatch{
		RepositoryID:  in.RepositoryID,
		Env:           in.Env,
		WorkflowFile:  workflowFile,
		Ref:           tag,
		Kind:          domain.DispatchKindRollback,
		RollbackOfSHA: target.HeadSHA,
		Actor:         in.Actor,
		State:         domain.DispatchStatePending,
	})
	if err != nil {
		return domain.DeployDispatch{}, fmt.Errorf("deployops: recording dispatch: %w", err)
	}

	if err := s.actions.DispatchWorkflow(ctx, owner, name, workflowFile, tag); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrProvider, err)
		_ = s.dispatches.Resolve(ctx, dispatch.ID, domain.DispatchStateAbandoned, nil)
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor,
			rollbackAuditDetail(map[string]string{"ref": tag, "workflow_file": workflowFile}, auth), wrapped)
		return domain.DeployDispatch{}, wrapped
	}

	s.recordAudit(ctx, in.RepositoryID, domain.OpsActionRollback, in.Env, in.Actor,
		rollbackAuditDetail(map[string]string{"ref": tag, "workflow_file": workflowFile, "rollback_of_sha": target.HeadSHA}, auth), nil)
	return dispatch, nil
}

func rollbackAuditDetail(base map[string]string, auth *AgentRollbackAuthorization) map[string]string {
	if auth == nil {
		return base
	}
	if base == nil {
		base = map[string]string{}
	}
	base["actor_kind"] = "agent"
	base["task_id"] = auth.TaskID.String()
	if auth.TaskKey != "" {
		base["task_key"] = auth.TaskKey
	}
	if auth.OwnedMergeSHA != "" {
		base["owned_merge_sha"] = auth.OwnedMergeSHA
	}
	if auth.Trigger != "" {
		base["trigger"] = auth.Trigger
	}
	if auth.AgentName != "" {
		base["agent"] = auth.AgentName
	}
	return base
}
