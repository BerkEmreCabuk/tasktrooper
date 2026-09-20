package board

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// QuotaSweeperInterval is how often a task parked on the Claude Code usage
// limit asks whether the limit has lifted.
//
// A minute, where the device sweeper waits ten. The two numbers answer
// different costs: the device probe is an HTTP call to a hub that may be at the
// far end of a home tunnel, while this is one indexed query against a partial
// index over a handful of rows (migration 101). What it buys is precision — the
// reset time is known to the second, and a coarse sweep would leave a task idle
// for most of a ten-minute window after its quota was already back.
const QuotaSweeperInterval = time.Minute

// QuotaResumeTaker hands back (and atomically unparks) one task whose Claude
// Code usage limit has expired, oldest park first.
//
// The clock is a parameter rather than read inside the store so a test can
// drive the sweep without waiting, and so one pass cannot see two different
// "now"s.
type QuotaResumeTaker interface {
	TakeQuotaResumable(ctx context.Context, now time.Time) (domain.BoardTask, bool, error)
}

// QuotaSweeper releases tasks parked on the local Claude Code subscription's
// usage limit. It is the DeviceSweeper's twin — same claim-and-redispatch
// shape, same one-task-per-pass pacing — with one difference that shapes
// everything else: there is nothing to probe.
//
// A phone announces nothing when it frees, but it can be ASKED. A subscription
// cannot: the only thing that knows when the window reopens is the message the
// CLI printed when it closed it, and the run that received that message wrote
// the time onto its own row before parking. So this sweeper's "probe" is the
// comparison the store does — is the recorded reset time in the past — and the
// state it reads is in the database rather than in this process, which is what
// makes a park survive a restart. A pod that dies between the park and the
// reset still wakes the task; nothing is held in memory to lose.
//
// It drains everything that is DUE in one pass, unlike the device sweeper. The
// device's one-per-pass pacing is about contention — one phone, so waking two
// tasks means one of them loses a race it paid a run's setup to enter. A quota
// reset has no such loser: every task whose recorded reset time has passed is
// due for the same reason at the same instant, and holding all but one back
// would idle them for a minute each for nothing. If the limit turns out to still
// be in force, the ones that re-park do so on the CLI's own fresh reset time,
// which is the only thing that can correct a wrong one anyway.
type QuotaSweeper struct {
	tasks      QuotaResumeTaker
	dispatcher *Dispatcher
	// now is injectable for the same reason it is on the store call: tests
	// drive the sweep across a reset boundary without sleeping.
	now func() time.Time
}

func NewQuotaSweeper(tasks QuotaResumeTaker, dispatcher *Dispatcher) *QuotaSweeper {
	return &QuotaSweeper{tasks: tasks, dispatcher: dispatcher, now: time.Now}
}

// Start runs the sweep on interval until ctx ends.
//
// It DOES sweep immediately on boot, unlike the device sweeper. The two differ
// because their state does: the device sweeper must not resume onto a phone
// another pod may be holding, so it waits one interval before trusting an idle
// hub. Here the reset time is a fact already written to the row — a restart
// changes nothing about it — and a pod that came back up after a long outage
// has parked tasks that are due right now.
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

// quotaSweepBatchCap bounds how many tasks one pass may resume.
//
// It is a brake on the pass, not a policy about quotas: the store hands back one
// task per statement and the loop keeps asking, so without a cap a single sweep
// could dispatch an unbounded number of runs — every task an outage parked —
// into the runner, in one tick. Twenty is far more than a
// realistic park backlog for one subscription and small enough that the
// remainder waits one minute, not an hour.
const quotaSweepBatchCap = 20

// sweep resumes every task whose reset time has passed, up to the batch cap.
// Separate from Start so tests can drive it without a clock.
//
// The claim stays one task per statement (FOR UPDATE SKIP LOCKED, oldest first):
// that is what keeps two pods sweeping at once from resuming the same card. The
// loop is only about not making a due task wait a whole interval behind another
// due task.
func (s *QuotaSweeper) sweep(ctx context.Context) {
	// One "now" for the whole pass, for the reason on the struct: a task must
	// not become due half-way through a sweep it was not due at the start of.
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
				"resumed":  "quota_reset",
				"resource": domain.ResourceClaudeCodeQuota,
				// The resume is the control plane's move, not a human's — without
				// these the board history shows an empty "Moved by User" row for a
				// move nobody made. Same reason the device sweeper sets them.
				domain.EventPayloadActor: domain.EventActorSystem,
				// The quota's OWN reason, not the device's. This said "device_free"
				// while resuming a subscription park, which is the one sentence the
				// history could not have meant.
				domain.EventPayloadReason: domain.MoveReasonQuotaRenewed,
			},
		}); err != nil {
			// The block is already cleared, so the task is back in its working
			// column either way; the reconciler picks up a task that never
			// started. The pass continues: one task the dispatcher could not
			// place must not hold back the others it can.
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("quota sweeper: redispatch failed")
			continue
		}
		log.Info().
			Str("task_id", task.ID.String()).
			Str("resource", domain.ResourceClaudeCodeQuota).
			Msg("parked task resumed: the agent cli usage limit has reset")
	}
	// Only reachable by exhausting the cap — every other exit returns above.
	log.Info().Int("cap", quotaSweepBatchCap).
		Msg("quota sweeper: per-pass cap reached, any remaining parked tasks resume on the next pass")
}
