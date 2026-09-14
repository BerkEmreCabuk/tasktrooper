package board

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// RunnerSweeperInterval is how often a task parked on an absent Mac asks
// whether that Mac has come back.
//
// Five minutes, between the deploy sweeper's two and the device sweeper's ten,
// and the number answers a human rhythm rather than a machine one: what this
// waits for is a person opening a laptop, and nobody notices the difference
// between "it started immediately" and "it started within five minutes" of
// sitting down. What they would notice is the cost of being eager — every
// pass is a round trip through the control plane to a home internet
// connection, per distinct member, forever, for every tenant on the fleet.
const RunnerSweeperInterval = 5 * time.Minute

// runnerSweepBatch bounds how many parked tasks one pass examines. It is a
// ceiling against a pathological state, not a working limit: the tasks are
// grouped by member before anything is probed, so a hundred parked cards
// belonging to one person cost one probe, not a hundred.
const runnerSweepBatch = 200

// RunnerSweeper releases tasks parked because the assignee's Mac was not
// connected.
//
// It is the DeploySweeper's shape — list without claiming, ask, claim only what
// is ready — with one difference that saves the whole design: **the probe is
// per MEMBER, not per task.** A deploy watch is genuinely per-task (two cards
// wait on two different deploys, either may settle first), but every card
// assigned to one person is waiting for exactly one laptop and they all become
// runnable at the same instant. Probing per card would multiply one fact by
// however many tasks that person has open.
//
// # There is nothing else to ask
//
// The device sweeper can ask a hub whether a phone is free; the quota sweeper
// can ask the clock. Neither has an equivalent here: the only thing that knows
// whether a Mac is attached is the control plane, which holds the tunnel, and
// it exposes no "is member X up?" endpoint. So the probe is the cheapest CALL
// the runner has — `preflight.report`, which reads a report the desktop app
// already pushed and touches no disk, spawns nothing and reaches no network.
// A Mac that answers it at all is a Mac that can take a run.
//
// That is also why an ERROR from the probe counts as attached. `preflight.
// report` can legitimately answer `not_ready` (the supervisor has not pushed a
// report yet), and a Mac that is up but not yet reporting is still a Mac: the
// run that follows will park again in seconds if it was wrong, which costs one
// dispatch, while treating it as absent would keep a connected machine's whole
// board parked on a technicality.
type RunnerSweeper struct {
	tasks      BlockedResourceLister
	tr         port.RunnerTransport
	dispatcher *Dispatcher
	// probeTimeout bounds one preflight call. A field so a test does not need
	// a real clock.
	probeTimeout time.Duration
}

// runnerProbeTimeout bounds one preflight.report. It is short on purpose: the
// call does no work on the far side, so anything slower than this is a tunnel
// that is up in name only, and a sweep that waited on it would hold the pass
// for every other member behind it.
const runnerProbeTimeout = 20 * time.Second

// NewRunnerSweeper returns nil when there is no transport, which is the correct
// state on a self-hosted or desktop install: nothing there can park on an
// absent Mac, so a sweeper for it would be a query per pass forever for a state
// that cannot occur.
func NewRunnerSweeper(tasks BlockedResourceLister, tr port.RunnerTransport, dispatcher *Dispatcher) *RunnerSweeper {
	if tasks == nil || tr == nil || !tr.Configured() || dispatcher == nil {
		return nil
	}
	return &RunnerSweeper{tasks: tasks, tr: tr, dispatcher: dispatcher, probeTimeout: runnerProbeTimeout}
}

// Start runs the sweep on interval until ctx ends.
//
// It does NOT sweep immediately on boot, unlike the quota and deploy sweepers.
// Those read a fact recorded outside this process — a reset time on a row,
// GitHub's view of a run — which a restart cannot change. This one reads the
// control plane's live tunnel registry, and a restart of THIS process says
// nothing about whether the Macs are up; sweeping in the first second of a
// rolling deploy would just aim a probe at every member of every tenant at
// once. One interval later the answer is the same and nothing is stampeding.
func (s *RunnerSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = RunnerSweeperInterval
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tenant.Sweep(ctx, "runner_not_attached", s.sweep)
			}
		}
	}()
	log.Info().Dur("interval", interval).Msg("offline-Mac sweeper started")
}

// sweep resumes every parked task whose member's Mac has come back. Separate
// from Start so tests can drive it without a clock.
func (s *RunnerSweeper) sweep(ctx context.Context) {
	parked, err := s.tasks.ListBlockedByResource(ctx, domain.ResourceRunnerNotAttached, runnerSweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("offline-Mac sweeper: listing parked tasks failed")
		return
	}
	if len(parked) == 0 {
		return
	}

	// One probe per member, and the answer is remembered for the whole pass.
	// Probing per task is the mistake this map exists to prevent.
	attached := map[string]bool{}
	for _, task := range parked {
		select {
		case <-ctx.Done():
			return
		default:
		}
		member := task.AssigneeUserID
		if member == "" {
			// A card parked on a Mac and then unassigned. Nothing will ever
			// release it here, so it is left for a human — the same treatment
			// ResourceHumanDecision gets, and for the same reason: an automatic
			// answer has run out.
			log.Warn().Str("task_id", task.ID.String()).
				Msg("offline-Mac sweeper: a parked task has no assignee, so no Mac can release it")
			continue
		}
		up, known := attached[member]
		if !known {
			up = s.probe(ctx, member)
			attached[member] = up
		}
		if !up {
			continue
		}
		s.resume(ctx, task)
	}
}

// probe asks whether this member's Mac is reachable.
func (s *RunnerSweeper) probe(ctx context.Context, member string) bool {
	ctx, cancel := context.WithTimeout(ctx, s.probeTimeout)
	defer cancel()

	_, err := s.tr.Do(ctx, member, port.RunnerCall{Method: "preflight.report"})
	if err == nil {
		return true
	}
	if block, ok := domain.RunnerBlockOf(err); ok && !block.NotReady {
		// The control plane's 409: there is no session for this member.
		return false
	}
	// Anything else — a runner-side error, a `not_ready`, a tunnel that broke
	// mid-probe — means something answered. See the type doc: a wrong "yes"
	// costs one dispatch that parks again, and a wrong "no" strands a working
	// machine's whole board.
	log.Debug().Err(err).Str("member", member).
		Msg("offline-Mac sweeper: the probe did not succeed cleanly; treating the Mac as back")
	return true
}

func (s *RunnerSweeper) resume(ctx context.Context, parked domain.BoardTask) {
	task, ok, err := s.tasks.TakeBlockedResourceTask(ctx, domain.ResourceRunnerNotAttached, parked.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", parked.ID.String()).Msg("offline-Mac sweeper: claiming a parked task failed")
		return
	}
	if !ok {
		// Claimed between the list and the take — another replica sweeping, or
		// a human dragging the card out of blocked. Both are fine.
		return
	}
	if err := s.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":  "runner_attached",
			"resource": domain.ResourceRunnerNotAttached,
			// The resume is the control plane's move, not a human's — without
			// these the board history shows an empty "Moved by User" row for a
			// move nobody made. Same reason every other sweeper sets them.
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonRunnerAttached,
		},
	}); err != nil {
		// The block is already cleared, so the task is back in its working
		// column either way; the reconciler picks up a task that never started.
		// The pass continues: one task the dispatcher could not place must not
		// hold back the others it can.
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("offline-Mac sweeper: redispatch failed")
		return
	}
	log.Info().
		Str("task_id", task.ID.String()).
		Str("member", task.AssigneeUserID).
		Msg("parked task resumed: the assignee's Mac is back")
}
