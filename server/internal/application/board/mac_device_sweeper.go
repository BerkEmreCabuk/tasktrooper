package board

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

type MemberDeviceProbe interface {
	ProbeMember(ctx context.Context, memberUID string) bool
}

type MacDeviceSweeper struct {
	tasks      BlockedResourceLister
	probe      MemberDeviceProbe
	dispatcher *Dispatcher
}

func NewMacDeviceSweeper(tasks BlockedResourceLister, probe MemberDeviceProbe, dispatcher *Dispatcher) *MacDeviceSweeper {
	if tasks == nil || probe == nil || dispatcher == nil {
		return nil
	}
	return &MacDeviceSweeper{tasks: tasks, probe: probe, dispatcher: dispatcher}
}

func (s *MacDeviceSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = DeviceSweeperInterval
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tenant.Sweep(ctx, "mac_mobile_device", s.sweep)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("Mac device sweeper started")
}

func (s *MacDeviceSweeper) sweep(ctx context.Context) {
	parked, err := s.tasks.ListBlockedByResource(ctx, domain.ResourceMobileDevice, runnerSweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("Mac device sweeper: listing parked tasks failed")
		return
	}
	if len(parked) == 0 {
		return
	}

	free := map[string]bool{}
	for _, task := range parked {
		select {
		case <-ctx.Done():
			return
		default:
		}
		member := task.AssigneeUserID
		if member == "" {
			log.Warn().Str("task_id", task.ID.String()).
				Msg("Mac device sweeper: a parked task has no assignee, so no Mac can release it")
			continue
		}
		ready, known := free[member]
		if !known {
			ready = s.probe.ProbeMember(ctx, member)
			free[member] = ready
		}
		if !ready {
			continue
		}

		free[member] = false
		s.resume(ctx, task)
	}
}

func (s *MacDeviceSweeper) resume(ctx context.Context, parked domain.BoardTask) {
	task, ok, err := s.tasks.TakeBlockedResourceTask(ctx, domain.ResourceMobileDevice, parked.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("Mac device sweeper: claiming a parked task failed")
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
			"resumed":                 "device_free",
			"resource":                domain.ResourceMobileDevice,
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonResourceFree,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("Mac device sweeper: redispatch failed")
		return
	}
	log.Info().
		Str("task_id", task.ID.String()).
		Str("member", task.AssigneeUserID).
		Msg("parked task resumed: a device on the assignee's Mac is free")
}
