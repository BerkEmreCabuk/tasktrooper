package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type StageEvidence interface {
	LatestVerdicts(ctx context.Context, taskID uuid.UUID) (map[string]string, error)
	// SetReviewPatchID stamps the diff a column's currently open span is being
	// approved on; called just before the move that closes it, so it lands on
	// the span the approval actually belongs to.
	SetReviewPatchID(ctx context.Context, taskID uuid.UUID, column, patchID string) error
	// LatestApprovedPatchID is the most recent CLOSED span of this column that
	// carries a patch id, and when it closed (its left_at) — the moment of
	// that approval. ok is false when the column was never approved this way.
	LatestApprovedPatchID(ctx context.Context, taskID uuid.UUID, column string) (patchID string, approvedAt time.Time, ok bool, err error)
}

func (s *Service) SetSpanStore(spans StageEvidence) {
	s.spans = spans
}

// recordCodeReviewApprovalPatchID stamps the current diff onto the code_review
// span that is about to close on this approving move. A patch id lookup
// failure (no git, no workspace) just leaves the span without one — the
// diff-skip stage that reads it back fails open on the same absence.
func (s *Service) recordCodeReviewApprovalPatchID(ctx context.Context, taskID uuid.UUID) {
	if s.spans == nil {
		return
	}
	patchID := s.currentTaskPatchID(ctx, taskID)
	if patchID == "" {
		return
	}
	if err := s.spans.SetReviewPatchID(ctx, taskID, string(domain.TaskColumnCodeReview), patchID); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("recording the code review approval's patch id failed")
	}
}

// LatestApprovedReviewPatchID is the diff-skip stage's read of the most
// recent approval this column closed on: what patch id it approved, and
// when. ok is false when the column has no such approval, the span store is
// unavailable, or the read failed — every one of those must fail the caller
// open (run the agent), never open (skip it).
func (s *Service) LatestApprovedReviewPatchID(ctx context.Context, taskID uuid.UUID, column domain.TaskColumn) (patchID string, approvedAt time.Time, ok bool) {
	if s.spans == nil {
		return "", time.Time{}, false
	}
	patchID, approvedAt, ok, err := s.spans.LatestApprovedPatchID(ctx, taskID, string(column))
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("reading the latest code review approval's patch id failed")
		return "", time.Time{}, false
	}
	return patchID, approvedAt, ok
}

func (s *Service) reviewChainGate(ctx context.Context, repo domain.Repository, task domain.BoardTask, prev, target domain.TaskColumn) error {
	if target != domain.TaskColumnDone && target != domain.TaskColumnReleased {
		return nil
	}

	if target == domain.TaskColumnReleased && prev == domain.TaskColumnDone {
		return nil
	}
	wf, err := s.workflow(ctx, task.TaskType)
	if err != nil {

		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review-chain gate could not read the task's workflow")
		return fmt.Errorf("%w: its workflow could not be read (%v) — retry the move", domain.ErrReviewChainIncomplete, err)
	}
	stages := wf.ReviewChain()
	if len(stages) == 0 {
		return nil
	}
	if s.spans == nil {
		return fmt.Errorf("%w: the column-span ledger is not available, so its review history cannot be read. "+
			"Fix the control plane's span store", domain.ErrReviewChainIncomplete)
	}
	verdicts, err := s.spans.LatestVerdicts(ctx, task.ID)
	if err != nil {

		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review-chain gate could not read span history")
		return fmt.Errorf("%w: its stage history could not be read (%v) — retry the move", domain.ErrReviewChainIncomplete, err)
	}

	var missing, rejected []string
	for _, stage := range stages {

		if !s.boardHasColumn(ctx, stage.Column) {
			continue
		}
		verdict, visited := verdicts[string(stage.Column)]
		switch {
		case !visited:
			missing = append(missing, fmt.Sprintf("%s (%s) — %s", stage.Label, stage.Column, stage.Remedy))
		case verdict == domain.ReviewVerdictReject:
			rejected = append(rejected, fmt.Sprintf("%s (%s) — %s", stage.Label, stage.Column, stage.Remedy))
		}
	}

	if len(rejected) > 0 {
		return fmt.Errorf("%w — cannot move %s to %s. Rejected at: %s",
			domain.ErrReviewStageRejected, taskLabel(task), target, strings.Join(rejected, "; "))
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w — cannot move %s to %s. Missing: %s",
			domain.ErrReviewChainIncomplete, taskLabel(task), target, strings.Join(missing, "; "))
	}
	return nil
}

func (s *Service) CheckReviewChain(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	if s.repos == nil || s.tasks == nil {
		return fmt.Errorf("repository store unavailable")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return err
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return err
	}

	return s.reviewChainGate(ctx, repo, task, task.Column, domain.TaskColumnDone)
}

func (s *Service) boardHasColumn(ctx context.Context, col domain.TaskColumn) bool {
	if s.columns == nil {
		return domain.ValidTaskColumn(col)
	}
	return s.columns.ValidateColumn(ctx, string(col)) == nil
}

func taskLabel(task domain.BoardTask) string {
	if strings.TrimSpace(task.Key) != "" {
		return task.Key
	}
	return task.ID.String()
}
