package deployops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultMonitorInterval = 2 * time.Minute

	dispatchMatchWindow = 15 * time.Minute

	existingRunLookupLimit = 100
)

type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

type dispatchableTarget struct {
	Repo         domain.Repository
	Env          string
	WorkflowFile string
}

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

		m.Sweep(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.Sweep(ctx)
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

type seenKey struct {
	repositoryID uuid.UUID
	workflowFile string
}

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

func matchDispatch(d domain.DeployDispatch, runs []port.ActionsRun) (port.ActionsRun, bool) {
	for _, r := range runs {
		if r.HeadBranch == d.Ref && !r.RunStartedAt.Before(d.CreatedAt) {
			return r, true
		}
	}
	return port.ActionsRun{}, false
}
