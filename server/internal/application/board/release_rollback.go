package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ReleaseRollbackDispatcher struct {
	dispatcher *Dispatcher
	tasks      ReleaseRollbackBoard
}

type ReleaseRollbackBoard interface {
	TaskRunbookReader
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

func NewReleaseRollbackDispatcher(dispatcher *Dispatcher, tasks ReleaseRollbackBoard) *ReleaseRollbackDispatcher {
	return &ReleaseRollbackDispatcher{dispatcher: dispatcher, tasks: tasks}
}

func (d *ReleaseRollbackDispatcher) DispatchReleaseRollback(ctx context.Context, attribution domain.ReleaseAttribution, incident domain.Incident, autoRollback bool) error {
	if d == nil || d.dispatcher == nil || d.tasks == nil {
		return nil
	}
	task, err := d.tasks.GetTask(ctx, incident.RepositoryID, attribution.TaskID)
	if err != nil {
		return fmt.Errorf("release rollback: reading the attributed task: %w", err)
	}
	// Only a released card can roll back; anything else is refused.
	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		log.Info().Str("task_id", task.ID.String()).Str("column", string(task.Column)).
			Msg("release rollback: attributed task is no longer in a released column, not dispatching")
		return nil
	}

	if _, err := d.tasks.AddComment(ctx, incident.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    releaseRollbackRunbook(task, incident, autoRollback),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release rollback: runbook comment failed")
	}

	return d.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: incident.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"release_rollback":                 true,
			"incident_id":                      incident.ID.String(),
			"auto_rollback":                    autoRollback,
			domain.EventPayloadResumedResource: domain.ResourceDeployWatch,
			domain.EventPayloadActor:           domain.EventActorSystem,
			domain.EventPayloadReason:          domain.MoveReasonResourceFree,
		},
	})
}

func releaseRollbackRunbook(task domain.BoardTask, incident domain.Incident, autoRollback bool) string {
	return releaseRollbackRunbookKey.Render(releaseRollbackRunbookInput{
		IncidentTitle:    incident.Title,
		IncidentEnv:      incident.Env,
		IncidentSeverity: string(incident.Severity),
		IncidentDetail:   strings.TrimSpace(incident.Detail),
		MergeSHA:         domain.ShortSHA(task.MergeCommitSHA),
		Runbook:          domain.TaskRollbackRunbookFields(task),
		AutoRollback:     autoRollback,
	})
}
