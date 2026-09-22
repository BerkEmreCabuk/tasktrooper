package deployops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var (
	ErrInvalidEnv = errors.New("invalid environment")

	ErrInvalidRunStatus = errors.New("run status must be in_progress or completed")

	ErrInvalidConclusion = errors.New("run conclusion must be success, failure or cancelled")

	ErrNotLocalRun = errors.New("run id does not belong to a locally recorded deploy")
)

type LocalRunInput struct {
	RepositoryID uuid.UUID
	Env          string

	RunID int64

	Command string

	HeadSHA string
	HeadRef string

	Status     string
	Conclusion string
	Actor      string
}

func (s *Service) RecordLocal(ctx context.Context, in LocalRunInput) (domain.DeploymentRun, error) {
	if _, err := s.repos.Get(ctx, in.RepositoryID); err != nil {

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

	if status == domain.RunStatusCompleted {
		var outcomeErr error
		if conclusion != domain.RunConclusionSuccess {
			outcomeErr = fmt.Errorf("local deploy %s", conclusion)
		}
		s.recordAudit(ctx, in.RepositoryID, domain.OpsActionDeploy, env, in.Actor, detail, outcomeErr)
	}
	return saved, nil
}
