package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// persistTimeout bounds a write that has to land even though the turn it belongs
// to has already been told to stop. Short: the caller has hung up, or the pod is
// on its way out.
const persistTimeout = 10 * time.Second

// persistCtx detaches a write from the cancellation that triggered it. The rows
// that matter most — the partial answer, the terminal status — are written
// exactly when the turn is being cancelled, so writing them on the turn's own
// context would drop precisely those and leave the chat with a stop that
// recorded nothing. Same reasoning as activity.Recorder.persistCtx.
func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

// runHandle is one chat turn in flight in this process: the context to cancel,
// the row to flip, and the one bit that says a human is the reason it stopped.
//
// That bit is what keeps a stop from being reported as a failure. Reading the
// contexts instead (runCtx cancelled, parent still alive — board/runner.go's
// stopRequested) cannot decide it here: the web client aborts its SSE request
// straight after asking for the cancel, and that abort cancels the parent too,
// so the two causes become indistinguishable a moment later.
type runHandle struct {
	sessionID uuid.UUID
	// runID is uuid.Nil when activity tracking is off — there is no row to flip
	// then, only a goroutine to stop.
	runID   uuid.UUID
	cancel  context.CancelFunc
	stopped atomic.Bool
}

// stop cancels the turn and reports whether this call was the one that did it.
// A second stop (a double-clicked button) answers false rather than cancelling
// anything twice.
func (h *runHandle) stop() bool {
	if !h.stopped.CompareAndSwap(false, true) {
		return false
	}
	h.cancel()
	return true
}

// userStopped reports whether a human stopped this turn, as opposed to the pod
// draining or the client hanging up. Nil-safe: a turn that never registered
// (activity tracking off in a test) was not stopped by anyone.
func (h *runHandle) userStopped() bool {
	return h != nil && h.stopped.Load()
}

// registerRun publishes the turn's cancel func for CancelSession to find and
// returns the closure that withdraws it. The turn owns both halves, so it cannot
// leave a cancel func behind pointing at a context nobody is running under any
// more — same shape as the board runner's registerCancel.
func (s *Service) registerRun(sessionID, runID uuid.UUID, cancel context.CancelFunc) (*runHandle, func()) {
	// No activity store means no run id to key on. A synthetic key keeps those
	// turns cancellable by session instead of having every one of them collide
	// on uuid.Nil.
	key := runID
	if key == uuid.Nil {
		key = uuid.New()
	}
	handle := &runHandle{sessionID: sessionID, runID: runID, cancel: cancel}

	s.runsMu.Lock()
	if s.runs == nil {
		s.runs = make(map[uuid.UUID]*runHandle)
	}
	if s.sessionRuns == nil {
		s.sessionRuns = make(map[uuid.UUID]map[uuid.UUID]struct{})
	}
	s.runs[key] = handle
	if s.sessionRuns[sessionID] == nil {
		s.sessionRuns[sessionID] = make(map[uuid.UUID]struct{})
	}
	s.sessionRuns[sessionID][key] = struct{}{}
	s.runsMu.Unlock()

	// The other half of a stop, and the half this process cannot be told about.
	// A stop request is served by whichever replica the load balancer picked,
	// and only THIS one holds the turn's cancel func, so the request writes
	// 'cancelled' on the row and this watches for it. Started here rather than
	// at the call sites so that registering a turn and being stoppable from
	// anywhere are the same act.
	stopWatch := s.watchRemoteCancel(handle)

	return handle, func() {
		stopWatch()
		s.runsMu.Lock()
		delete(s.runs, key)
		if keys := s.sessionRuns[sessionID]; keys != nil {
			delete(keys, key)
			if len(keys) == 0 {
				delete(s.sessionRuns, sessionID)
			}
		}
		s.runsMu.Unlock()
	}
}

// remoteCancelPoll is how often a live turn re-reads its own row, and therefore
// how long a stop takes to reach the replica executing it.
//
// A chat turn is interactive: the user is watching tokens arrive and expects
// the stop button to bite. Five seconds is one read per turn per five seconds —
// nothing beside the model call it is interrupting — and there is no cheaper
// signal available, because unlike a board run a chat turn has no heartbeat
// write to piggyback on (session_runs has no updated_at, and adding one would
// be a write per turn per interval to carry a value nothing else reads).
const remoteCancelPoll = 5 * time.Second

// watchRemoteCancel polls the turn's row and stops the turn when it says
// cancelled. Returns the closure that ends the watch.
//
// It is a no-op when there is no row to watch (activity tracking off), which is
// also the shape every existing test runs in.
func (s *Service) watchRemoteCancel(handle *runHandle) func() {
	if s.activityStore == nil || handle == nil || handle.runID == uuid.Nil {
		return func() {}
	}
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	go func() {
		every := s.cancelPoll
		if every <= 0 {
			every = remoteCancelPoll
		}
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), remoteCancelPoll)
				status, err := s.activityStore.RunStatus(ctx, handle.runID)
				cancel()
				if err != nil {
					// A failed read says nothing about the row. Killing a live
					// turn over a transient pool error would be worse than the
					// bug this loop closes.
					log.Warn().Err(err).Str("run_id", handle.runID.String()).
						Msg("chat turn cancel watch failed")
					continue
				}
				if status == domain.SessionRunStatusCancelled {
					log.Info().Str("run_id", handle.runID.String()).
						Msg("chat turn cancelled elsewhere in the fleet, stopping it here")
					handle.stop()
					return
				}
			}
		}
	}()
	return stop
}

// liveRuns snapshots the session's in-flight turns. A snapshot rather than work
// under the lock: cancelling wakes a turn's own goroutine, and the first thing it
// does on its way out is take runsMu to unregister itself.
func (s *Service) liveRuns(sessionID uuid.UUID) []*runHandle {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	keys := s.sessionRuns[sessionID]
	handles := make([]*runHandle, 0, len(keys))
	for key := range keys {
		if handle := s.runs[key]; handle != nil {
			handles = append(handles, handle)
		}
	}
	return handles
}

// CancelSession stops the chat turns this session has in flight and reports
// whether it had any.
//
// False is an ordinary answer, not an error: the turn may have finished a
// moment before the button was pressed, which is a race the caller cannot win
// from the UI it rendered. Only a session that does not exist is an error.
//
// Normally there is exactly one turn — the composer is disabled while a send is
// in flight — but nothing in the API forbids two, so this stops all of them.
func (s *Service) CancelSession(ctx context.Context, sessionID uuid.UUID, reason string) (bool, error) {
	if _, err := s.store.Get(ctx, sessionID); err != nil {
		return false, fmt.Errorf("%w: %v", domain.ErrSessionNotFound, err)
	}
	handles := s.liveRuns(sessionID)
	if len(handles) == 0 {
		// No handle HERE does not mean no turn anywhere. The turn is very
		// likely streaming on another replica, which cannot be reached and does
		// not need to be: flipping its row is the whole of a stop, and the
		// replica executing it is watching that row (watchRemoteCancel). Before
		// this, a stop that landed on the wrong pod returned "nothing was
		// running" and the session kept going.
		return s.cancelSessionRows(ctx, sessionID)
	}
	live := false
	for _, handle := range handles {
		stopped, err := s.stopRun(ctx, handle, reason)
		if err != nil {
			return live, err
		}
		live = live || stopped
	}
	return live, nil
}

// CancelSessionRun stops ONE named turn of the session. Same outcome vocabulary
// as CancelSession; the session id is not decoration but the scope check — a run
// id alone would let any turn in the system be stopped by naming a session the
// caller happens to hold.
func (s *Service) CancelSessionRun(ctx context.Context, sessionID, runID uuid.UUID, reason string) (bool, error) {
	if _, err := s.store.Get(ctx, sessionID); err != nil {
		return false, fmt.Errorf("%w: %v", domain.ErrSessionNotFound, err)
	}
	s.runsMu.Lock()
	handle := s.runs[runID]
	s.runsMu.Unlock()
	if handle == nil || handle.sessionID != sessionID {
		// Not ours to stop directly — see CancelSession. The scope check moves
		// into the row listing, which is what proves the run belongs to this
		// session rather than to one the caller merely named.
		return s.cancelSessionRow(ctx, sessionID, runID)
	}
	return s.stopRun(ctx, handle, reason)
}

// cancelSessionRows flips every running row of the session. It is what a stop
// does when the turn is executing somewhere this process cannot reach.
func (s *Service) cancelSessionRows(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	if s.activityStore == nil {
		return false, nil
	}
	runs, err := s.activityStore.ListRunsBySession(ctx, sessionID, activeRunLookback)
	if err != nil {
		return false, err
	}
	live := false
	for _, run := range runs {
		if run.Status != domain.SessionRunStatusRunning {
			continue
		}
		flipped, err := s.activityStore.CancelRun(ctx, run.ID)
		if err != nil {
			return live, err
		}
		live = live || flipped
	}
	return live, nil
}

// cancelSessionRow flips one named run, having first proved it belongs to this
// session. The scope check is not decoration: a run id alone would let any turn
// in the tenant be stopped by naming a session the caller happens to hold.
func (s *Service) cancelSessionRow(ctx context.Context, sessionID, runID uuid.UUID) (bool, error) {
	if s.activityStore == nil {
		return false, nil
	}
	runs, err := s.activityStore.ListRunsBySession(ctx, sessionID, activeRunLookback)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if run.ID != runID {
			continue
		}
		if run.Status != domain.SessionRunStatusRunning {
			return false, nil
		}
		return s.activityStore.CancelRun(ctx, runID)
	}
	return false, nil
}

// activeRunLookback bounds the row listing a cross-replica stop reads. A
// session's live turns are always its most recent ones — the composer is
// disabled while a send is in flight — so a short window finds every one of
// them and never walks a long transcript.
const activeRunLookback = 20

// stopRun writes the durable half of a stop, then the live half.
//
// The atomic UPDATE goes first because it is what decides the outcome: the
// request races the turn itself, and waking the goroutine is only worth doing
// for the caller that won. The row is also the half that survives this process,
// which is what stops a completed-but-cancelled turn from being re-reported as
// running by the activity endpoints.
func (s *Service) stopRun(ctx context.Context, handle *runHandle, reason string) (bool, error) {
	flipped := false
	if s.activityStore != nil && handle.runID != uuid.Nil {
		var err error
		flipped, err = s.activityStore.CancelRun(ctx, handle.runID)
		if err != nil {
			return false, err
		}
	}
	stopped := handle.stop()
	if stopped {
		log.Info().
			Str("session_id", handle.sessionID.String()).
			Str("run_id", handle.runID.String()).
			Str("reason", strings.TrimSpace(reason)).
			Msg("chat turn stopped by user")
	}
	return stopped || flipped, nil
}

// persistCancelledTurn keeps whatever the agent had already said when it was
// stopped. Half an answer is still an answer — the user watched it arrive — and
// dropping it would leave the transcript with a question and nothing after it.
// Nothing is written when nothing was streamed.
func (s *Service) persistCancelledTurn(ctx context.Context, sessionID uuid.UUID, partial string) {
	partial = strings.TrimSpace(partial)
	if partial == "" {
		return
	}
	writeCtx, cancel := persistCtx(ctx)
	defer cancel()
	s.persistAssistantResponse(writeCtx, sessionID, domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: partial},
	})
}
