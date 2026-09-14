package board

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

const DeploySweeperInterval = 2 * time.Minute

const deploySweepBatch = 25

type BlockedResourceLister interface {
	ListBlockedByResource(ctx context.Context, resource string, limit int) ([]domain.BoardTask, error)
	TakeBlockedResourceTask(ctx context.Context, resource string, taskID uuid.UUID) (domain.BoardTask, bool, error)
}

type DeployStatusResolver interface {
	StatusForTask(ctx context.Context, task domain.BoardTask) (domain.DeployWatchStatus, error)
}

type DeploySweeper struct {
	tasks      BlockedResourceLister
	watch      DeployStatusResolver
	dispatcher *Dispatcher
}

func NewDeploySweeper(tasks BlockedResourceLister, watch DeployStatusResolver, dispatcher *Dispatcher) *DeploySweeper {
	return &DeploySweeper{tasks: tasks, watch: watch, dispatcher: dispatcher}
}

func (s *DeploySweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.tasks == nil || s.watch == nil || s.dispatcher == nil {
		return
	}
	if interval <= 0 {
		interval = DeploySweeperInterval
	}
	go func() {
		tenant.Sweep(ctx, "deploy_watch", s.sweep)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tenant.Sweep(ctx, "deploy_watch", s.sweep)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("deploy watch sweeper started")
}

func (s *DeploySweeper) sweep(ctx context.Context) {
	parked, err := s.tasks.ListBlockedByResource(ctx, domain.ResourceDeployWatch, deploySweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("deploy sweeper: listing parked tasks failed")
		return
	}
	for _, task := range parked {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.resumeIfSettled(ctx, task)
	}
}

func (s *DeploySweeper) resumeIfSettled(ctx context.Context, parked domain.BoardTask) {
	status, err := s.watch.StatusForTask(ctx, parked)
	if err != nil {
		log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("deploy sweeper: resolving deploy status failed")
		return
	}
	if !status.State.Settled() {
		return
	}
	task, ok, err := s.tasks.TakeBlockedResourceTask(ctx, domain.ResourceDeployWatch, parked.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("deploy sweeper: claiming a parked task failed")
		return
	}
	if !ok {
		return
	}
	if err := s.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":                          "deploy_settled",
			"resource":                         domain.ResourceDeployWatch,
			"deploy_state":                     string(status.State),
			domain.EventPayloadResumedResource: domain.ResourceDeployWatch,
			domain.EventPayloadActor:           domain.EventActorSystem,
			domain.EventPayloadReason:          domain.MoveReasonResourceFree,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("deploy sweeper: redispatch failed")
		return
	}
	log.Info().
		Str("task_id", task.ID.String()).
		Str("deploy_state", string(status.State)).
		Msg("parked task resumed: its deploy has finished")
}
