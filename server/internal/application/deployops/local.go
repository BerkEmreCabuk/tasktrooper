package deployops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Locally-driven deploys.
//
// The console's picture of "what is live where" is a mirror of GitHub Actions
// runs, which is exactly right while Actions runs them. It goes blind the
// moment somebody ships from a laptop instead — the break-glass path each repo
// documents in .ai/local-deploy.md, taken while Actions is blocked (today:
// unpaid account) — and a blind console is worse than no console, because the
// grid still shows the last GitHub run as if it were current.
//
// So a local deploy reports itself: RecordLocal at the start (an in_progress
// run) and again at the end (completed, with its conclusion). Nothing here
// deploys anything; it records what a script on somebody's machine already
// did, and the rows it writes are ordinary deployment_runs — same matrix, same
// history list, same rollback lookup — distinguished only by
// domain.TriggerSourceLocal and a negative run id.
var (
	// ErrInvalidEnv means the env is not one of domain.DeployEnvs().
	ErrInvalidEnv = errors.New("invalid environment")
	// ErrInvalidRunStatus means Status was neither in_progress nor completed.
	ErrInvalidRunStatus = errors.New("run status must be in_progress or completed")
	// ErrInvalidConclusion means a completed run's conclusion was not one of
	// success | failure | cancelled.
	ErrInvalidConclusion = errors.New("run conclusion must be success, failure or cancelled")
	// ErrNotLocalRun means the caller passed a run id that is not a local
	// run's. Refusing it is what stops a finish report from rewriting a real
	// Actions run's row.
	ErrNotLocalRun = errors.New("run id does not belong to a locally recorded deploy")
)

// LocalRunInput is one report about a deploy driven from somebody's machine.
// RunID zero means "this is the start" and mints a new run; a non-zero RunID
// updates the run that start returned.
type LocalRunInput struct {
	RepositoryID uuid.UUID
	Env          string
	// RunID is the id RecordLocal returned for this deploy's start report.
	// Zero opens a new run.
	RunID int64
	// Command is what was actually run, e.g. "scripts/release-local.sh web".
	// It lands in the run's WorkflowFile column, which is where the UI already
	// looks for "what produced this run".
	Command string
	// HeadSHA/HeadRef are the commit that was built. The scripts build
	// origin/main by default, so this is what `git rev-parse origin/main`
	// said, not the dirty working tree.
	HeadSHA string
	HeadRef string
	// Status is in_progress or completed; Conclusion is required by the
	// latter and must be empty for the former.
	Status     string
	Conclusion string
	Actor      string
}

// RecordLocal writes (or updates) the deployment run of a break-glass deploy.
//
// The update path merges onto the stored row rather than overwriting it: the
// store's upsert takes head_sha, head_ref and workflow_file straight from the
// incoming values, so a finish report that carried only a conclusion would
// otherwise blank what the start report recorded.
func (s *Service) RecordLocal(ctx context.Context, in LocalRunInput) (domain.DeploymentRun, error) {
	if _, err := s.repos.Get(ctx, in.RepositoryID); err != nil {
		// Same rule as Dispatch: an unknown repository propagates unwrapped so
		// the caller can 404 rather than audit an attempt against nothing.
		return domain.DeploymentRun{}, fmt.Errorf("deployops: loading repository: %w", err)
	}
	env := strings.TrimSpace(in.Env)
	if !domain.ValidDeployEnv(env) {
		return domain.DeploymentRun{}, ErrInvalidEnv
	}

	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = domain.RunStatusInProgress
	}
	conclusion := strings.TrimSpace(in.Conclusion)
	switch status {
	case domain.RunStatusInProgress, domain.RunStatusQueued:
		// A running deploy has no outcome yet; keeping one anyway would put a
		// green tick on the grid for a deploy still in flight.
		conclusion = ""
	case domain.RunStatusCompleted:
		switch conclusion {
		case domain.RunConclusionSuccess, domain.RunConclusionFailure, domain.RunConclusionCancelled:
		default:
			return domain.DeploymentRun{}, ErrInvalidConclusion
		}
	default:
		return domain.DeploymentRun{}, ErrInvalidRunStatus
	}

	now := s.now()
	run := domain.DeploymentRun{
		RepositoryID:  in.RepositoryID,
		Env:           env,
		WorkflowFile:  strings.TrimSpace(in.Command),
		HeadSHA:       strings.TrimSpace(in.HeadSHA),
		HeadRef:       strings.TrimSpace(in.HeadRef),
		Status:        status,
		Conclusion:    conclusion,
		TriggerSource: domain.TriggerSourceLocal,
		TriggeredBy:   strings.TrimSpace(in.Actor),
		// No HTMLURL: there is no Actions run to link to, and the UI renders
		// the SHA as plain text when this is empty rather than as a dead link.
		StartedAt: &now,
	}

	switch {
	case in.RunID == 0:
		run.RunID = domain.LocalRunID(now)
	case !domain.IsLocalRun(in.RunID):
		return domain.DeploymentRun{}, ErrNotLocalRun
	default:
		stored, err := s.runs.ByRunID(ctx, in.RepositoryID, in.RunID)
		if err != nil {
			return domain.DeploymentRun{}, fmt.Errorf("deployops: loading local run: %w", err)
		}
		if stored.TriggerSource != domain.TriggerSourceLocal {
			return domain.DeploymentRun{}, ErrNotLocalRun
		}
		run.RunID = stored.RunID
		run.StartedAt = stored.StartedAt
		// Merge: whatever this report left empty keeps what start recorded.
		if run.WorkflowFile == "" {
			run.WorkflowFile = stored.WorkflowFile
		}
		if run.HeadSHA == "" {
			run.HeadSHA = stored.HeadSHA
		}
		if run.HeadRef == "" {
			run.HeadRef = stored.HeadRef
		}
		if run.TriggeredBy == "" {
			run.TriggeredBy = stored.TriggeredBy
		}
		run.RollbackOfSHA = stored.RollbackOfSHA
	}
	if status == domain.RunStatusCompleted {
		run.CompletedAt = &now
	}

	saved, err := s.runs.Upsert(ctx, run)
	detail := map[string]string{
		"source":  domain.TriggerSourceLocal,
		"command": run.WorkflowFile,
		"status":  status,
	}
	if conclusion != "" {
		detail["conclusion"] = conclusion
	}
	if run.HeadSHA != "" {
		detail["head_sha"] = run.HeadSHA
	}
	if err != nil {
		wrapped := fmt.Errorf("deployops: recording local run: %w", err)
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, env, in.Actor, detail, wrapped)
		return domain.DeploymentRun{}, wrapped
	}
	// Only the finish report is audited as an action: a start and its finish
	// are one deploy, and logging both would double every break-glass deploy
	// in the audit trail.
	if status == domain.RunStatusCompleted {
		var outcomeErr error
		if conclusion != domain.RunConclusionSuccess {
			outcomeErr = fmt.Errorf("local deploy %s", conclusion)
		}
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, env, in.Actor, detail, outcomeErr)
	}
	return saved, nil
}
