package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ReleaseRollbackDispatcher wakes the agent that owns a released card so it can
// roll its release back — or, when auto_rollback is off, so a human finds the
// proposal on the card.
//
// It is a dispatch and not a direct rollback call, and that is the design
// decision rather than an implementation detail. A server-side automatic
// rollback would undo a release at a moment nothing had read the task's own
// rollback plan, and the mechanical half is the EASY half: `git revert` puts the
// code back, and then somebody still has to reverse a migration, turn a flag off
// and say on the card which of those actually happened. Only a run can do that,
// which is why the automatic path wakes the same agent, in the same column, with
// the same tool the manual path uses.
type ReleaseRollbackDispatcher struct {
	dispatcher *Dispatcher
	tasks      ReleaseRollbackBoard
}

// ReleaseRollbackBoard is the board slice this needs: re-read the attributed
// task (the incident carries only its id) and put the runbook on it.
type ReleaseRollbackBoard interface {
	TaskRunbookReader
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

func NewReleaseRollbackDispatcher(dispatcher *Dispatcher, tasks ReleaseRollbackBoard) *ReleaseRollbackDispatcher {
	return &ReleaseRollbackDispatcher{dispatcher: dispatcher, tasks: tasks}
}

// DispatchReleaseRollback puts the rollback runbook on the card and wakes its
// owner.
//
// The runbook comment is how the task's own rollback_plan / before_deploy /
// after_deploy reach the run. Those fields were written by the developer who
// made the change and, until this, were only ever read on the way OUT
// (postPreDeployChecklist when the deploy was dispatched, postDeployNotes when
// it succeeded). Feeding them back in on the way BACK is the whole point: the
// run that rolls the release back is the run that needs them most, and the
// runner already injects the task's recent comments into a run's context, so a
// comment is the channel that exists rather than a new one.
//
// autoRollback=false still dispatches. The card gets the proposal, the agent
// reports it, and the tool it would call refuses with ErrRollbackAutoDisabled —
// which is the right outcome: a human reads a written-up rollback on the card
// instead of a silence, and nothing was executed.
func (d *ReleaseRollbackDispatcher) DispatchReleaseRollback(ctx context.Context, attribution domain.ReleaseAttribution, incident domain.Incident, autoRollback bool) error {
	if d == nil || d.dispatcher == nil || d.tasks == nil {
		return nil
	}
	task, err := d.tasks.GetTask(ctx, incident.RepositoryID, attribution.TaskID)
	if err != nil {
		return fmt.Errorf("release rollback: reading the attributed task: %w", err)
	}
	// Only a released card can have a release rolled back. Anything else is an
	// attribution that has gone stale (the card was dragged back through review
	// after it shipped) and waking an agent there would put a rollback run on a
	// task somebody is already reworking.
	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		log.Info().Str("task_id", task.ID.String()).Str("column", string(task.Column)).
			Msg("release rollback: attributed task is no longer in a released column, not dispatching")
		return nil
	}

	if _, err := d.tasks.AddComment(ctx, incident.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    releaseRollbackRunbook(task, incident, autoRollback),
	}); err != nil {
		// The comment IS the run's context, so losing it makes the dispatch
		// much less useful — but a rollback run with no runbook still beats no
		// rollback run at all, so this is logged rather than fatal.
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release rollback: runbook comment failed")
	}

	return d.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: incident.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"release_rollback": true,
			"incident_id":      incident.ID.String(),
			"auto_rollback":    autoRollback,
			// The same resume marker the deploy sweeper writes, and for the same
			// reason: it is the only key that opens the terminal-column
			// suspension (deployWatchWake). Nothing else can produce it.
			domain.EventPayloadResumedResource: domain.ResourceDeployWatch,
			domain.EventPayloadActor:           domain.EventActorSystem,
			domain.EventPayloadReason:          domain.MoveReasonResourceFree,
		},
	})
}

// releaseRollbackRunbook is the text the rollback run reads.
//
// It states four things in this order because that is the order they have to be
// acted on: what broke, what the developer said to do about it, what this
// system will do mechanically, and what it explicitly will NOT do. The last one
// is the one that must never be dropped — a rollback that reports itself
// complete while a migration is still applied is worse than one that says it
// could not finish.
func releaseRollbackRunbook(task domain.BoardTask, incident domain.Incident, autoRollback bool) string {
	var sb strings.Builder
	sb.WriteString("ROLLBACK REQUIRED — this task's release is what production is running, and production is unhealthy.\n\n")
	sb.WriteString(fmt.Sprintf("Incident: %s (%s, severity %s)\n", incident.Title, incident.Env, incident.Severity))
	if detail := strings.TrimSpace(incident.Detail); detail != "" {
		sb.WriteString(detail + "\n")
	}
	sb.WriteString(fmt.Sprintf("Released commit: %s\n\n", domain.ShortSHA(task.MergeCommitSHA)))

	if runbook := domain.TaskRollbackRunbook(task); runbook != "" {
		sb.WriteString(runbook)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("This task recorded NO rollback plan. Say so explicitly when you report — the absence is itself a finding for the next release.\n\n")
	}

	if autoRollback {
		sb.WriteString("auto_rollback is ON for this environment: call rollback_task_release with trigger=health_incident. " +
			"It will undo the code — by re-deploying the last good commit where a deploy workflow exists, or by reverting the merge commit on the default branch where the host deploys on push. ")
	} else {
		sb.WriteString("auto_rollback is OFF for this environment: call rollback_task_release anyway — it will execute NOTHING and return the written-up proposal (`proposed: true`). " +
			"That is the correct outcome here. Post what it returns on this task, say plainly that a human has to confirm it, and stop. Do not look for another way to roll production back. ")
	}
	sb.WriteString("Then work through the plan above yourself and report every step you performed AND every step you could not — a schema change, a feature flag, anything with a human on the other end. " +
		"Do not report the rollback as complete unless it is.")
	return sb.String()
}
