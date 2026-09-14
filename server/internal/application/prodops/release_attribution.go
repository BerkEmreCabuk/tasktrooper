package prodops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ReleaseAttributor names the task whose release an incident belongs to.
//
// It is implemented by application/deploywatch and injected rather than
// imported, so this package keeps depending on nothing but domain and its own
// stores — the same arrangement TaskBoard and DeployHistory already have here.
type ReleaseAttributor interface {
	// AttributeRelease answers "which task's merge commit is this environment
	// running, and did it go live recently enough for this incident to be its
	// fault". ok=false means it could not attribute, which is never an error:
	// the generic remedy still fires and still says something useful.
	AttributeRelease(ctx context.Context, repositoryID uuid.UUID, env string, onset time.Time) (domain.ReleaseAttribution, bool)
	// HealthWindow is how long that ownership lasts, for the wording.
	HealthWindow() time.Duration
}

// ReleaseRollbackDispatcher starts the rollback of an attributed release. It is
// the board's Dispatcher, narrowed: prodops does not roll anything back itself,
// it wakes the agent that owns the card and lets the rollback happen where every
// other release action happens — inside a run, with the task's own rollback plan
// in front of it and a tool call that is audited.
type ReleaseRollbackDispatcher interface {
	DispatchReleaseRollback(ctx context.Context, attribution domain.ReleaseAttribution, incident domain.Incident, autoRollback bool) error
}

// SetReleaseAttributor wires the commit-keyed attribution. Without it the
// incident path behaves exactly as it did: a generic deploy correlation with no
// task on it.
func (s *Service) SetReleaseAttributor(a ReleaseAttributor) { s.attributor = a }

// SetReleaseRollbackDispatcher wires the automatic rollback. Without it an
// attributed incident is still labelled with its task, it just does not start
// anything.
func (s *Service) SetReleaseRollbackDispatcher(d ReleaseRollbackDispatcher) { s.rollbacks = d }

// attributeAndMaybeRollBack is the health-window half of the release loop,
// called from Ingest once an incident is open.
//
// The existing correlation in remedy.go answers a question about the
// ENVIRONMENT — "did some deploy finish in the last 45 minutes" — and so can
// only ever produce the sentence "roll back the last release", with nothing to
// click and nobody to ask. This answers a question about a COMMIT: production
// is running SHA X, X is task DE-12's merge commit, DE-12 went live nine
// minutes ago, and DE-12 has a rollback plan and an owning role. That is the
// difference between an advisory and an action.
//
// What happens next is the deploy target's `auto_rollback` flag, which until now
// was decorative — read in exactly one place (remedy.go) to reword a sentence:
//
//	true  → wake the agent that owns the card, with the incident and the task's
//	        rollback plan, to execute the rollback.
//	false → attribute the incident, write the proposal on the card, notify, and
//	        wait for a human. The HTTP rollback endpoint's typed confirmation is
//	        unchanged and is what that human uses.
//
// Everything here is best-effort and nothing in it can fail the ingest. An
// incident that is recorded but not attributed is still an incident; an incident
// lost because the attribution failed is a production outage nobody hears about.
func (s *Service) attributeAndMaybeRollBack(ctx context.Context, incident domain.Incident) domain.Incident {
	if s.attributor == nil || incident.RepositoryID == uuid.Nil {
		return incident
	}
	onset := incident.FirstSeenAt
	if onset.IsZero() {
		onset = incident.LastSeenAt
	}
	attribution, ok := s.attributor.AttributeRelease(ctx, incident.RepositoryID, incident.Env, onset)
	if !ok {
		return incident
	}

	autoRollback := s.autoRollbackEnabled(ctx, incident.RepositoryID, incident.Env)
	s.event(ctx, incident.ID, domain.IncidentEventTriaged, releaseAttributionNote(attribution, s.attributor.HealthWindow(), autoRollback))

	// The card gets the same sentence, because the card is where the release's
	// own history lives and where the agent about to roll it back will read it.
	if s.tasks != nil {
		if _, err := s.tasks.AddComment(ctx, incident.RepositoryID, attribution.TaskID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content: fmt.Sprintf("Production incident inside this release's health window.\n\n%s\n\nIncident: %s (%s, %s)\n%s",
				releaseAttributionNote(attribution, s.attributor.HealthWindow(), autoRollback),
				incident.Title, incident.Env, incident.Severity, strings.TrimSpace(incident.Detail)),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", attribution.TaskID.String()).Msg("release attribution comment failed")
		}
	}

	if s.rollbacks == nil {
		return incident
	}
	if err := s.rollbacks.DispatchReleaseRollback(ctx, attribution, incident, autoRollback); err != nil {
		log.Warn().Err(err).Str("task_id", attribution.TaskID.String()).Str("incident_id", incident.ID.String()).
			Msg("dispatching the release rollback failed")
	}
	return incident
}

// autoRollbackEnabled reads the deploy target's policy. A missing target or an
// unreadable one is FALSE: "roll production back without asking" is not a
// default anything should fall into because a lookup failed.
func (s *Service) autoRollbackEnabled(ctx context.Context, repositoryID uuid.UUID, env string) bool {
	if s.targets == nil {
		return false
	}
	target, err := s.targets.Get(ctx, repositoryID, "", env)
	if err != nil {
		return false
	}
	return target.AutoRollback
}

func releaseAttributionNote(a domain.ReleaseAttribution, window time.Duration, autoRollback bool) string {
	gap := time.Since(a.DeployedAt).Round(time.Minute)
	note := fmt.Sprintf("Attributed to release %s (%s): its merge commit %s is what %s is running, deployed %s ago — inside the %s post-release window.",
		a.TaskKey, a.Title, domain.ShortSHA(a.MergeSHA), a.Env, humanDuration(gap), humanDuration(window))
	if autoRollback {
		return note + " auto_rollback is ON for this target: the rollback is being executed."
	}
	return note + " auto_rollback is OFF for this target: the rollback is proposed and needs a human to confirm it."
}
