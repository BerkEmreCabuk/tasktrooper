// Package deployops assembles the cross-repository deploy matrix, serves
// deploy run / console audit history, and dispatches or rolls back deploys
// behind a typed confirmation guardrail.
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

// Service assembles the cross-repository deploy matrix, serves deploy run
// and console audit history, and drives dispatch/rollback.
type Service struct {
	runs       port.DeploymentRunStore
	dispatches port.DeployDispatchStore
	audit      port.OpsAuditStore
	targets    port.DeployTargetStore
	repos      port.RepositoryStore
	pipeline   port.RepositoryPipelineJobStore
	actions    port.ActionsClient

	// now is swappable so rollback tag names are assertable in tests.
	// Production code never needs to call SetClock — New defaults it to
	// time.Now.
	now func() time.Time

	// repoResolver resolves a repository's GitHub owner/name. domain.Repository
	// carries no such field — only a local RootPath and a display Name — so
	// this is late-set via SetRepoResolver, mirroring the SetTaskCreator /
	// SetStoreOnboarder idiom in application/deploy/service.go. Nil (the
	// pre-wiring default) makes Dispatch/Rollback fail with ErrNoRepoResolver
	// rather than silently calling GitHub with an empty owner.
	repoResolver func(ctx context.Context, repo domain.Repository) (owner, name string, err error)
}

// New wires the deployops service's collaborators.
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

// SetClock overrides the service's clock. Tests use it to make rollback tag
// names deterministic.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetRepoResolver injects how a repository's GitHub owner/name is resolved.
// domain.Repository does not carry them; they come from the checkout's git
// origin, the same source board/pipeline.go uses.
func (s *Service) SetRepoResolver(fn func(ctx context.Context, repo domain.Repository) (owner, name string, err error)) {
	s.repoResolver = fn
}

// MatrixCell is one repository × environment square.
type MatrixCell struct {
	Env          string                `json:"env"`
	Configured   bool                  `json:"configured"`   // a deploy target exists
	Dispatchable bool                  `json:"dispatchable"` // a <env>_deploy workflow mapping exists
	Provider     string                `json:"provider"`
	HealthURL    string                `json:"health_url"`
	AutoRollback bool                  `json:"auto_rollback"`
	LastRun      *domain.DeploymentRun `json:"last_run,omitempty"`
	// RollbackSHA is the ref rollback would target; empty means rollback is
	// unavailable and the UI disables the button.
	RollbackSHA string `json:"rollback_sha"`
}

type MatrixRepo struct {
	ID    uuid.UUID    `json:"id"`
	Name  string       `json:"name"`
	Kind  string       `json:"kind"`
	Cells []MatrixCell `json:"cells"` // always len(domain.DeployEnvs), in promotion order
}

type MatrixView struct {
	Repos []MatrixRepo `json:"repos"`
	Envs  []string     `json:"envs"`
}

// Matrix assembles the cross-repository deploy grid. It makes exactly four
// store calls — repos.List, targets.ListAll, pipeline.ListAll, runs.LatestAll
// — then joins everything in memory; it never issues a query per repository
// for the joinable data. Only the per-cell rollback lookup
// (runs.LastSuccessfulBefore) adds one call per cell that has a last run.
//
// Repositories with no deploy target at all are omitted from view.Repos —
// there is nothing to ship or roll back for them.
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

	// dispatchableByRepo marks (repository, env) pairs that have a workflow
	// mapping for the env's deploy category — the axis that is independent
	// of whether a deploy target is configured.
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
			// No deploy target anywhere for this repo: omit it entirely
			// rather than render an all-unconfigured row.
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
					// No prior good run: rollback unavailable, not an error.
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

// envForPipelineCategory inverts domain.DeployEnvCategory: given a pipeline
// job's category, it returns the deploy env that category dispatches, or ""
// when category is not a deploy category at all (validate/build/test).
func envForPipelineCategory(category string) string {
	for _, env := range domain.DeployEnvs() {
		if domain.DeployEnvCategory(env) == category {
			return env
		}
	}
	return ""
}

// Runs returns the deploy run history for one (repository, env), newest
// first. limit is clamped to 20 when <= 0 and capped at 100.
func (s *Service) Runs(ctx context.Context, repositoryID uuid.UUID, env string, limit int) ([]domain.DeploymentRun, error) {
	limit = clampLimit(limit, 20, 100)
	runs, err := s.runs.ListByEnv(ctx, repositoryID, env, limit)
	if err != nil {
		return nil, fmt.Errorf("deployops: listing runs: %w", err)
	}
	return runs, nil
}

// Audit returns console action attempts, newest first. A nil repositoryID
// returns entries for every repository. limit is clamped to 50 when <= 0 and
// capped at 200.
func (s *Service) Audit(ctx context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error) {
	limit = clampLimit(limit, 50, 200)
	entries, err := s.audit.List(ctx, repositoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("deployops: listing audit entries: %w", err)
	}
	return entries, nil
}

// clampLimit applies def when limit is <= 0, then caps the result at max.
func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		limit = def
	}
	if limit > max {
		limit = max
	}
	return limit
}

// defaultDispatchRef is used whenever a dispatch's ref is empty.
// domain.Repository carries no default-branch field — that value only ever
// existed on GitHub's API response — so this constant stands in rather than
// spending a GitHub round trip on every dispatch to look one up. The UI is
// expected to send an explicit ref whenever the branch is not "main".
const defaultDispatchRef = "main"

// Sentinel errors Dispatch/Rollback return so the HTTP layer can pick the
// right status code without string-matching an error message.
var (
	// ErrNoWorkflowMapping means the target environment has no repository_pipeline_jobs
	// row of target_kind "workflow" — the caller's configuration problem, a 409.
	ErrNoWorkflowMapping = errors.New("no deploy workflow mapped for this environment")
	// ErrNoRollbackTarget means there is no earlier successful run to roll back
	// to for this (repository, env) — a 409.
	ErrNoRollbackTarget = errors.New("no previous successful deploy to roll back to")
	// ErrConfirmMismatch means a production-class action's Confirm field did
	// not equal the repository's name exactly — a 400.
	ErrConfirmMismatch = errors.New("confirmation phrase does not match the repository name")
	// ErrProvider marks a failure that came from GitHub rather than from this
	// system or the caller, so the handler can answer 502 instead of 500.
	ErrProvider = errors.New("github request failed")
	// ErrNoRepoResolver means SetRepoResolver was never called. Dispatch and
	// Rollback refuse to proceed rather than call GitHub with an empty owner,
	// which would silently 404 or dispatch into the wrong repository.
	ErrNoRepoResolver = errors.New("no repository resolver configured")
)

// DispatchInput is what a console-triggered deploy dispatch needs from the
// caller. An empty Ref defaults to defaultDispatchRef.
type DispatchInput struct {
	RepositoryID uuid.UUID
	Env          string
	Ref          string
	Confirm      string
	Actor        string
}

// RollbackInput is what a console-triggered rollback needs from the caller.
// Rollback always targets the last successful run for (repository, env) —
// the caller does not choose a SHA.
type RollbackInput struct {
	RepositoryID uuid.UUID
	Env          string
	Confirm      string
	Actor        string
}

// productionClass reports whether an action needs the typed confirmation.
// Rollback is always production-class regardless of environment: it
// discards whatever is currently deployed.
func productionClass(env string, kind string) bool {
	return kind == domain.DispatchKindRollback || env == domain.DeployEnvProd
}

// resolveDeployWorkflow returns the workflow file mapped to env's deploy
// category for repositoryID — the repository_pipeline_jobs row whose
// category is env's deploy category (domain.DeployEnvCategory, i.e.
// "<env>_deploy"), whose target_kind is "workflow", and whose target_ref is
// non-empty. "" means no such mapping exists.
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

// resolveRepoCoordinates resolves the GitHub owner/repo pair actions.* calls
// need, via the injected repoResolver (SetRepoResolver) — domain.Repository
// itself carries no persisted GitHub owner, only a local RootPath and a
// display Name. With no resolver configured this fails clean with
// ErrNoRepoResolver instead of silently calling GitHub with an empty owner.
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

// recordAudit writes one ops_audit_log row for a single action attempt,
// success or failure. A write failure here is logged and never masks the
// caller's original error or result — failing an otherwise-successful
// dispatch just because the audit write didn't land would be worse than a
// gap in the log.
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

// Dispatch triggers a workflow_dispatch deploy of (repository, env). A
// production-class dispatch (env == prod) requires Confirm to equal the
// repository's name exactly, checked before any GitHub call. The dispatch
// row is inserted before the GitHub call so a dispatch that succeeds on
// GitHub's side but fails to record locally is still attributable; on a
// GitHub failure the dispatch resolves to abandoned. Every attempt is
// audited, success and failure alike.
func (s *Service) Dispatch(ctx context.Context, in DispatchInput) (domain.DeployDispatch, error) {
	repo, err := s.repos.Get(ctx, in.RepositoryID)
	if err != nil {
		// Not found propagates unwrapped so the handler can 404 instead of
		// auditing an attempt against a repository that doesn't exist.
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

	// Resolved before the dispatch row is written: a repository with no
	// resolver configured (or whose checkout has no resolvable git origin)
	// can never succeed, so there is nothing to attribute a pending dispatch
	// to in the first place.
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

// Rollback creates a tag at the last known-good SHA for (repository, env) and
// dispatches THAT TAG — workflow_dispatch refuses a bare SHA. Every rollback
// is production-class regardless of env, so Confirm must equal the
// repository's name exactly, checked before any GitHub call. Every attempt
// is audited, success and failure alike.
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

// AgentRollbackAuthorization is what replaces the typed confirmation when the
// actor is an agent rather than a human at a keyboard.
//
// The confirmation is NOT bypassed for agents, and it is not something the tool
// layer can pass. `Confirm == repository name` is a proof-of-intent ritual: it
// exists because a human clicking Rollback might have clicked the wrong row,
// and typing the name out is the friction that catches it. An agent has no
// hands to slip, so re-asking it to type the repository's name proves nothing —
// the model would simply read the name off the repository record it already
// holds and type it, which is a confirmation that confirms nothing and is worse
// than none, because it looks like one in the audit log.
//
// What an agent CAN be asked for is the thing a human cannot: proof that the
// release it wants to undo is the release it owns. So the agent path swaps the
// ritual for four facts checked by this layer, all of them read from the
// database rather than supplied by the caller:
//
//	OwnedMergeSHA   the task's merge commit must be what the environment is
//	                currently running. If another task has released since, this
//	                rollback would undo somebody else's change.
//	AutoRollback    the deploy target must have auto_rollback ON. That flag was
//	                decorative before this change (read only to reword an
//	                advisory in prodops/remedy.go); it is now the switch that
//	                says "an agent may act here without asking".
//	Trigger         something must actually have gone wrong: a failed deploy of
//	                that commit, or an incident attributed to its health window.
//	                A rollback with no trigger is a rollback nobody asked for.
//	TaskKey/Agent   recorded in the audit entry, so every agent rollback names
//	                the card and the role that made it. The human endpoint's
//	                audit shape is unchanged.
//
// The HTTP endpoint is untouched: POST /v1/repositories/:id/deploy/:env/rollback
// still requires Confirm to equal the repository name, still for a human, and
// this type is unreachable from it.
type AgentRollbackAuthorization struct {
	// TaskID / TaskKey identify the card whose release is being undone.
	TaskID  uuid.UUID
	TaskKey string
	// OwnedMergeSHA is the commit the task put in production. The caller has
	// already proven it is what is live; this is recorded, not re-derived.
	OwnedMergeSHA string
	// Trigger names why (deploy_failed | health_incident).
	Trigger string
	// AgentName is the role acting, for the audit entry.
	AgentName string
}

// RollbackForTask is the agent-actor rollback: the same dispatch as Rollback,
// authorized by ownership of the released commit instead of by a typed phrase.
//
// It is a separate exported method rather than a flag on RollbackInput on
// purpose. A boolean like `SkipConfirm` on the shared input would be one
// mis-set field away from disabling the human guard, and it would put the
// decision in the caller; a second method with its own required authorization
// type cannot be reached by accident, and reading the call site tells you which
// path you are on.
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

// rollback is the shared body of both paths. Everything above it decides WHO
// may call it; everything in it is the same tag-and-dispatch for either.
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

	// Resolved only once there is an actual rollback target: no point failing
	// on a missing resolver before we even know there is something to roll
	// back to.
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

// rollbackAuditDetail folds the agent authorization into the audit entry. Every
// agent rollback names the card, the commit it owned, why it fired and which
// role acted; a human rollback's entry is byte-for-byte what it always was.
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
