package board

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

const DeviceSweeperInterval = 10 * time.Minute

type ResourceProbe interface {
	Probe(ctx context.Context) bool
}

type BlockedResourceTaker interface {
	TakeBlockedByResource(ctx context.Context, resource string) (domain.BoardTask, bool, error)
}

type DeviceSweeper struct {
	tasks      BlockedResourceTaker
	probe      ResourceProbe
	dispatcher *Dispatcher
}

func NewDeviceSweeper(tasks BlockedResourceTaker, probe ResourceProbe, dispatcher *Dispatcher) *DeviceSweeper {
	return &DeviceSweeper{tasks: tasks, probe: probe, dispatcher: dispatcher}
}

func (s *DeviceSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.tasks == nil || s.probe == nil || s.dispatcher == nil {
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
				tenant.Sweep(ctx, "mobile_device", s.sweep)
			}
		}
	}()
}

func (s *DeviceSweeper) sweep(ctx context.Context) {
	if !s.probe.Probe(ctx) {
		return
	}
	task, ok, err := s.tasks.TakeBlockedByResource(ctx, domain.ResourceMobileDevice)
	if err != nil {
		log.Warn().Err(err).Msg("device sweeper: claiming a parked task failed")
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
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("device sweeper: redispatch failed")
		return
	}
	log.Info().
		Str("task_id", task.ID.String()).
		Str("resource", domain.ResourceMobileDevice).
		Msg("parked task resumed: the shared device is free")
}
