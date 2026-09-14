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

// AttributeRelease answers "which task's code is production running, and did it
// go live recently enough for this incident to be its fault".
//
// It is the health-window half of the watch, read from the other end. The
// existing correlation (prodops/remedy.go, deployCorrelationWindow) asks a
// question about the ENVIRONMENT — "did some deploy finish in the last 45
// minutes" — and can therefore only ever produce the sentence "roll back the
// last release". This asks a question about a COMMIT: the deployment run that
// is live carries a head SHA, that SHA is some task's merge_commit_sha, and
// that task has a key, a rollback plan and an owning agent.
//
// Two conditions, both required, because either one alone attributes wrongly:
//
//	the run must be SUCCESSFUL and the newest for this env — a failed deploy
//	    left production on the PREVIOUS commit, and blaming the task whose
//	    deploy never landed would roll back a release that is not there.
//	it must have finished within the health window before the incident — after
//	    that the release has proven itself and the next outage is the
//	    environment's own, not this card's.
//
// Returns ok=false rather than an error whenever it cannot attribute. Nothing
// downstream of this may fail because a blame could not be assigned; the
// generic remedy still fires and still says something useful.
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

// attributionRunLookback bounds the deployment-run read. A repository deploys
// a handful of times a day at most, so twenty rows reach far past any health
// window worth attributing inside.
const attributionRunLookback = 20

// liveRun picks the deploy that put the current code in production: the newest
// SUCCESSFUL run that finished at or before onset, and only if it finished
// inside the window.
//
// Runs later than onset are skipped rather than disqualifying: a deploy that
// started after an incident opened cannot have caused it, but it also does not
// mean nothing before it did.
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
