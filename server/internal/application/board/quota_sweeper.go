package board

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const QuotaSweeperInterval = time.Minute

type QuotaResumeTaker interface {
	TakeQuotaResumable(ctx context.Context, now time.Time) (domain.BoardTask, bool, error)
}

// Nothing to probe: the reset time is on the run's own row, so the park survives restarts - nothing is held in memory.
type QuotaSweeper struct {
	tasks      QuotaResumeTaker
	dispatcher *Dispatcher
	now        func() time.Time
}

func NewQuotaSweeper(tasks QuotaResumeTaker, dispatcher *Dispatcher) *QuotaSweeper {
	return &QuotaSweeper{tasks: tasks, dispatcher: dispatcher, now: time.Now}
}

func (s *QuotaSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.tasks == nil || s.dispatcher == nil {
		return
	}
	if interval <= 0 {
		interval = QuotaSweeperInterval
	}
	go func() {
		s.sweep(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweep(ctx)
			}
		}
	}()
}

const quotaSweepBatchCap = 20

func (s *QuotaSweeper) sweep(ctx context.Context) {
	now := s.now()
	for range quotaSweepBatchCap {
		task, ok, err := s.tasks.TakeQuotaResumable(ctx, now)
		if err != nil {
			log.Warn().Err(err).Msg("quota sweeper: claiming a parked task failed")
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
				"resumed":                 "quota_reset",
				"resource":                domain.ResourceClaudeCodeQuota,
				domain.EventPayloadActor:  domain.EventActorSystem,
				domain.EventPayloadReason: domain.MoveReasonQuotaRenewed,
			},
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("quota sweeper: redispatch failed")
			continue
		}
		log.Info().
			Str("task_id", task.ID.String()).
			Str("resource", domain.ResourceClaudeCodeQuota).
			Msg("parked task resumed: the agent cli usage limit has reset")
	}
	log.Info().Int("cap", quotaSweepBatchCap).
		Msg("quota sweeper: per-pass cap reached, any remaining parked tasks resume on the next pass")
}
