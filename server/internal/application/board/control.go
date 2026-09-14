package board

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultCancelReason = "run stopped by user"

type RunControl interface {
	Cancel(runID uuid.UUID) bool
	Enqueue(job RunJob)
}

type Controller struct {
	runs   port.TaskAgentRunStore
	events port.BoardEventStore
	tasks  port.BoardTaskStore
	runner RunControl
}

func NewController(runs port.TaskAgentRunStore, events port.BoardEventStore, tasks port.BoardTaskStore, runner RunControl) *Controller {
	return &Controller{runs: runs, events: events, tasks: tasks, runner: runner}
}

func (c *Controller) CancelRun(ctx context.Context, repositoryID, taskID, runID uuid.UUID, reason string) (domain.TaskAgentRun, error) {
	run, err := c.runs.GetByID(ctx, runID)
	if err != nil {
		return domain.TaskAgentRun{}, err
	}

	if run.TaskID != taskID {
		return domain.TaskAgentRun{}, domain.ErrTaskAgentRunNotFound
	}

	if _, err := c.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskAgentRun{}, err
	}
	if reason == "" {
		reason = defaultCancelReason
	}

	cancelled, ok, err := c.runs.CancelIfLive(ctx, runID, reason)
	if err != nil {
		return domain.TaskAgentRun{}, err
	}
	if !ok {
		return domain.TaskAgentRun{}, domain.ErrRunNotLive
	}

	c.runner.Cancel(runID)

	if err := c.tasks.BlockOnCancel(ctx, repositoryID, taskID, reason); err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Str("run_id", runID.String()).
			Msg("cancel run: block task failed")
	}
	payload, err := json.Marshal(map[string]interface{}{
		"run_id":                 runID.String(),
		"agent_id":               run.AgentID.String(),
		"reason":                 reason,
		domain.EventPayloadActor: domain.EventActorHuman,
	})
	if err == nil {
		var actorUserID *string
		if uid := registry.ActorUserIDFromContext(ctx); uid != "" {
			actorUserID = &uid
		}
		_, err = c.events.Create(ctx, domain.BoardEvent{
			RepositoryID: repositoryID,
			TaskID:       taskID,
			EventType:    domain.BoardEventTaskRunCancelled,
			Payload:      payload,
			ActorUserID:  actorUserID,
		})
	}
	if err != nil {
		log.Error().Err(err).Str("task_id", taskID.String()).Str("run_id", runID.String()).
			Msg("cancel run: record event failed")
	}
	return cancelled, nil
}

func (c *Controller) RerunRun(ctx context.Context, repositoryID, taskID, runID uuid.UUID) (domain.TaskAgentRun, error) {
	run, err := c.runs.GetByID(ctx, runID)
	if err != nil {
		return domain.TaskAgentRun{}, err
	}
	if run.TaskID != taskID {
		return domain.TaskAgentRun{}, domain.ErrTaskAgentRunNotFound
	}
	if !domain.TaskAgentRunIsTerminal(run.Status) {
		return domain.TaskAgentRun{}, domain.ErrRunNotTerminal
	}
	task, err := c.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskAgentRun{}, err
	}

	if task.BlockedAt != nil {
		return domain.TaskAgentRun{}, domain.ErrTaskBlockedForRerun
	}
	payload, err := json.Marshal(map[string]interface{}{
		"source_run_id":          runID.String(),
		"agent_id":               run.AgentID.String(),
		domain.EventPayloadActor: domain.EventActorHuman,
	})
	if err != nil {
		return domain.TaskAgentRun{}, err
	}
	var actorUserID *string
	if uid := registry.ActorUserIDFromContext(ctx); uid != "" {
		actorUserID = &uid
	}

	event, err := c.events.Create(ctx, domain.BoardEvent{
		RepositoryID: repositoryID,
		TaskID:       taskID,
		EventType:    domain.BoardEventTaskRerunRequested,
		Payload:      payload,
		ActorUserID:  actorUserID,
	})
	if err != nil {
		return domain.TaskAgentRun{}, err
	}
	fresh, err := c.runs.Create(ctx, domain.TaskAgentRun{
		TaskID:       taskID,
		AgentID:      run.AgentID,
		BoardEventID: event.ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	if err != nil {
		return domain.TaskAgentRun{}, err
	}
	c.runner.Enqueue(RunJob{
		Run:          fresh,
		Event:        event,
		Task:         task,
		RepositoryID: repositoryID,
		Tenant:       tenantOf(ctx),
	})
	return fresh, nil
}
