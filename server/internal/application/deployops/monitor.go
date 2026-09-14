package deployops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	// defaultMonitorInterval is Start's fallback when the caller passes a
	// non-positive interval — same guard storeops.Monitor.Start applies.
	defaultMonitorInterval = 2 * time.Minute
	// dispatchMatchWindow is how long a pending dispatch waits for its run
	// before it is abandoned. A run that has not appeared in GitHub within
	// this window is not going to.
	dispatchMatchWindow = 15 * time.Minute
	// existingRunLookupLimit bounds the read of already-stored runs used to
	// tell "newly failed" from "already known failed" apart — generous
	// relative to a single GitHub page (30 runs) so the run this sweep just
	// fetched is never truncated out of the comparison set.
	existingRunLookupLimit = 100
)

// IncidentIngester is the Ingest side of the incident service, narrowed
// locally so this package does not import application/prodops — same shape
// storeops.Monitor uses.
type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

// dispatchableTarget is one repository × environment pair that has BOTH a
// deploy target AND a workflow mapping for that env's deploy category — the
// bounded polling scope the Global Constraint requires: every sweep is at
// most one GitHub API call per dispatchable target, never per repository.
type dispatchableTarget struct {
	Repo         domain.Repository
	Env          string
	WorkflowFile string
}

// dispatchableTargets returns every (repository, env) pair worth polling —
// the same axis Matrix's Dispatchable flag reports (Configured AND a
// workflow mapping), resolved here as a concrete list rather than a per-cell
// bool. It makes exactly three store calls, never one per repository.
func (s *Service) dispatchableTargets(ctx context.Context) ([]dispatchableTarget, error) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("deployops: listing repositories: %w", err)
	}
	reposByID := make(map[uuid.UUID]domain.Repository, len(repos))
	for _, r := range repos {
		reposByID[r.ID] = r
	}

	targets, err := s.targets.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("deployops: listing deploy targets: %w", err)
	}
	configured := make(map[uuid.UUID]map[string]bool, len(targets))
	for _, t := range targets {
		if configured[t.RepositoryID] == nil {
			configured[t.RepositoryID] = map[string]bool{}
		}
		configured[t.RepositoryID][t.Env] = true
	}

	jobs, err := s.pipeline.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("deployops: listing pipeline jobs: %w", err)
	}

	var out []dispatchableTarget
	for _, j := range jobs {
		if j.TargetKind != domain.PipelineTargetWorkflow || j.TargetRef == "" {
			continue
		}
		env := envForPipelineCategory(j.Category)
		if env == "" || !configured[j.RepositoryID][env] {
			continue
		}
		repo, ok := reposByID[j.RepositoryID]
		if !ok {
			continue
		}
		out = append(out, dispatchableTarget{Repo: repo, Env: env, WorkflowFile: j.TargetRef})
	}
	return out, nil
}

// Monitor polls every dispatchable (repository, env) pair's GitHub Actions
// runs, mirrors them into deployment_runs, reconciles pending console
// dispatches against the runs it just saw, and turns a newly failed run into
// a "deploy" incident. It is deployops' counterpart to storeops.Monitor.
type Monitor struct {
	svc      *Service
	ingester IncidentIngester

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewMonitor(svc *Service, ingester IncidentIngester) *Monitor {
	return &Monitor{svc: svc, ingester: ingester}
}

// Start runs a sweep every interval until the context is cancelled. Calling
// Start on a running monitor is a no-op. Mirrors storeops.Monitor.Start.
func (m *Monitor) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultMonitorInterval
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// Sweep immediately — waiting a full interval before the first pass
		// leaves a fresh dispatch or a failed deploy unwatched for no reason.
		tenant.Sweep(ctx, "deploy_runs", m.Sweep)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tenant.Sweep(ctx, "deploy_runs", m.Sweep)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("deploy monitor started")
}

func (m *Monitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}

// seenKey identifies the set of GitHub Actions runs a sweep fetched for one
// (repository, workflow file) pair — reconciliation only ever matches a
// pending dispatch against runs THIS sweep saw, never a stale set left over
// from an earlier pass.
type seenKey struct {
	repositoryID uuid.UUID
	workflowFile string
}

// Sweep runs one pass: list every dispatchable target's recent runs, mirror
// them locally, reconcile pending dispatches against what was just seen, and
// ingest an incident for any run newly observed as failed. It is
// best-effort: one target's failure is logged and the sweep moves on rather
// than aborting the rest, matching storeops.Monitor.Sweep's contract.
func (m *Monitor) Sweep(ctx context.Context) {
	targets, err := m.svc.dispatchableTargets(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("deploy monitor: listing dispatchable targets failed")
		return
	}

	seen := make(map[seenKey][]port.ActionsRun, len(targets))
	for _, target := range targets {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.sweepTarget(ctx, target, seen)
	}

	m.reconcileDispatches(ctx, seen)
}

// sweepTarget lists target's recent Actions runs, mirrors each into
// deployment_runs as externally triggered, and ingests an incident for any
// run this call newly observes as completed+failure. A run's prior state is
// read BEFORE the upsert so "newly failed" can be told apart from "already
// known failed" — Upsert itself only ever returns the just-written row, not
// what preceded it.
func (m *Monitor) sweepTarget(ctx context.Context, target dispatchableTarget, seen map[seenKey][]port.ActionsRun) {
	owner, name, err := m.svc.resolveRepoCoordinates(ctx, target.Repo)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", target.Repo.ID.String()).Str("env", target.Env).
			Msg("deploy monitor: resolving repository coordinates failed")
		return
	}

	runs, err := m.svc.actions.ListWorkflowRuns(ctx, owner, name, target.WorkflowFile, "")
	if err != nil {
		log.Warn().Err(err).Str("repository_id", target.Repo.ID.String()).Str("env", target.Env).
			Msg("deploy monitor: listing workflow runs failed")
		return
	}
	seen[seenKey{target.Repo.ID, target.WorkflowFile}] = runs
	if len(runs) == 0 {
		return
	}

	existing, err := m.svc.runs.ListByEnv(ctx, target.Repo.ID, target.Env, existingRunLookupLimit)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", target.Repo.ID.String()).Str("env", target.Env).
			Msg("deploy monitor: listing existing runs failed")
		return
	}
	existingByRunID := make(map[int64]domain.DeploymentRun, len(existing))
	for _, r := range existing {
		existingByRunID[r.RunID] = r
	}

	for _, r := range runs {
		prior, hadPrior := existingByRunID[r.ID]

		var startedAt, completedAt *time.Time
		if !r.RunStartedAt.IsZero() {
			started := r.RunStartedAt
			startedAt = &started
		}
		if r.Status == domain.RunStatusCompleted && !r.UpdatedAt.IsZero() {
			completed := r.UpdatedAt
			completedAt = &completed
		}

		if _, err := m.svc.runs.Upsert(ctx, domain.DeploymentRun{
			RepositoryID:  target.Repo.ID,
			Env:           target.Env,
			RunID:         r.ID,
			RunNumber:     r.RunNumber,
			WorkflowFile:  target.WorkflowFile,
			HeadSHA:       r.HeadSHA,
			HeadRef:       r.HeadBranch,
			Status:        r.Status,
			Conclusion:    r.Conclusion,
			HTMLURL:       r.HTMLURL,
			TriggerSource: domain.TriggerSourceExternal,
			StartedAt:     startedAt,
			CompletedAt:   completedAt,
		}); err != nil {
			log.Warn().Err(err).Int64("run_id", r.ID).Str("repository_id", target.Repo.ID.String()).
				Msg("deploy monitor: upserting deployment run failed")
			continue
		}

		newlyFailed := r.Status == domain.RunStatusCompleted && r.Conclusion == domain.RunConclusionFailure &&
			!(hadPrior && prior.Conclusion == domain.RunConclusionFailure)
		if newlyFailed {
			m.ingestFailure(ctx, target, r)
		}
	}
}

// ingestFailure records a "deploy" incident for a newly failed run. A nil
// ingester (no incident wiring) is tolerated, not an error.
func (m *Monitor) ingestFailure(ctx context.Context, target dispatchableTarget, r port.ActionsRun) {
	if m.ingester == nil {
		return
	}
	if _, err := m.ingester.Ingest(ctx, domain.IncidentInput{
		RepositoryID: target.Repo.ID,
		Env:          target.Env,
		Source:       domain.IncidentSourceDeploy,
		Fingerprint:  domain.IncidentFingerprint("deploy", target.Env, target.WorkflowFile),
		Title:        fmt.Sprintf("Deploy failed: %s %s", target.Repo.Name, target.Env),
		Detail:       fmt.Sprintf("GitHub Actions run %d (%s) completed with conclusion %q.", r.ID, target.WorkflowFile, r.Conclusion),
		Severity:     domain.IncidentSeverityHigh,
		Payload: map[string]any{
			"run_id":        r.ID,
			"workflow_file": target.WorkflowFile,
			"html_url":      r.HTMLURL,
			"head_sha":      r.HeadSHA,
		},
	}); err != nil {
		log.Warn().Err(err).Str("repository_id", target.Repo.ID.String()).Str("env", target.Env).
			Msg("deploy monitor: incident ingest failed")
	}
}

// reconcileDispatches attributes each pending dispatch to the run this sweep
// saw for it, or abandons it once dispatchMatchWindow has passed with no
// match.
func (m *Monitor) reconcileDispatches(ctx context.Context, seen map[seenKey][]port.ActionsRun) {
	pending, err := m.svc.dispatches.ListPending(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("deploy monitor: listing pending dispatches failed")
		return
	}

	for _, d := range pending {
		runs := seen[seenKey{d.RepositoryID, d.WorkflowFile}]
		if matched, ok := matchDispatch(d, runs); ok {
			triggerSource := domain.TriggerSourceUI
			if d.Kind == domain.DispatchKindRollback {
				triggerSource = domain.TriggerSourceRollback
			}
			if err := m.svc.runs.Stamp(ctx, d.RepositoryID, matched.ID, triggerSource, d.Actor, d.RollbackOfSHA); err != nil {
				log.Warn().Err(err).Str("dispatch_id", d.ID.String()).
					Msg("deploy monitor: stamping matched run failed")
				continue
			}
			runID := matched.ID
			if err := m.svc.dispatches.Resolve(ctx, d.ID, domain.DispatchStateMatched, &runID); err != nil {
				log.Warn().Err(err).Str("dispatch_id", d.ID.String()).
					Msg("deploy monitor: resolving matched dispatch failed")
			}
			continue
		}

		if time.Since(d.CreatedAt) > dispatchMatchWindow {
			if err := m.svc.dispatches.Resolve(ctx, d.ID, domain.DispatchStateAbandoned, nil); err != nil {
				log.Warn().Err(err).Str("dispatch_id", d.ID.String()).
					Msg("deploy monitor: abandoning stale dispatch failed")
			}
		}
	}
}

// matchDispatch finds the run in runs that dispatch d most plausibly caused:
// same head branch as the ref d dispatched, started at or after d was
// created. A run that started before the dispatch existed cannot be its run.
func matchDispatch(d domain.DeployDispatch, runs []port.ActionsRun) (port.ActionsRun, bool) {
	for _, r := range runs {
		if r.HeadBranch == d.Ref && !r.RunStartedAt.Before(d.CreatedAt) {
			return r, true
		}
	}
	return port.ActionsRun{}, false
}
