package deploywatch

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (s *Service) AttributeRelease(ctx context.Context, repositoryID uuid.UUID, env string, onset time.Time) (domain.ReleaseAttribution, bool) {
	if s.runs == nil || s.tasks == nil || repositoryID == uuid.Nil {
		return domain.ReleaseAttribution{}, false
	}
	if onset.IsZero() {
		onset = s.now()
	}
	if strings.TrimSpace(env) == "" {
		env = domain.DeployEnvProd
	}

	runs, err := s.runs.ListByEnv(ctx, repositoryID, env, attributionRunLookback)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("env", env).
			Msg("deploy watch: listing deployment runs for attribution failed")
		return domain.ReleaseAttribution{}, false
	}

	live, ok := liveRun(runs, onset, s.healthWindow)
	if !ok {
		return domain.ReleaseAttribution{}, false
	}

	task, err := s.tasks.FindTaskByMergeCommit(ctx, repositoryID, live.HeadSHA)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("sha", domain.ShortSHA(live.HeadSHA)).
				Msg("deploy watch: resolving the task behind a deployed commit failed")
		}
		return domain.ReleaseAttribution{}, false
	}
	finished := runFinishedAt(live)
	return domain.ReleaseAttribution{
		TaskID:     task.ID,
		TaskKey:    task.Key,
		Title:      task.Title,
		MergeSHA:   live.HeadSHA,
		Env:        env,
		DeployedAt: finished,
	}, true
}

const attributionRunLookback = 20

func liveRun(runs []domain.DeploymentRun, onset time.Time, window time.Duration) (domain.DeploymentRun, bool) {
	var best domain.DeploymentRun
	found := false
	for _, r := range runs {
		if r.Status != domain.RunStatusCompleted || r.Conclusion != domain.RunConclusionSuccess {
			continue
		}
		if strings.TrimSpace(r.HeadSHA) == "" {
			continue
		}
		finished := runFinishedAt(r)
		if finished.IsZero() || finished.After(onset) {
			continue
		}
		if onset.Sub(finished) > window {
			continue
		}
		if !found || finished.After(runFinishedAt(best)) {
			best = r
			found = true
		}
	}
	return best, found
}

func runFinishedAt(r domain.DeploymentRun) time.Time {
	if r.CompletedAt != nil && !r.CompletedAt.IsZero() {
		return *r.CompletedAt
	}
	return r.UpdatedAt
}
