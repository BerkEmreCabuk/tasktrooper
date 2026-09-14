package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// stubQuotaTaker stands in for the store's one atomic claim-and-unpark
// statement. It records the clock it was given, because the due check happening
// in the store — not in the sweeper — is the whole design.
type stubQuotaTaker struct {
	tasks []domain.BoardTask
	calls int
	asked []time.Time
	err   error
}

func (s *stubQuotaTaker) TakeQuotaResumable(_ context.Context, now time.Time) (domain.BoardTask, bool, error) {
	s.calls++
	s.asked = append(s.asked, now)
	if s.err != nil {
		return domain.BoardTask{}, false, s.err
	}
	if len(s.tasks) == 0 {
		return domain.BoardTask{}, false, nil
	}
	task := s.tasks[0]
	s.tasks = s.tasks[1:]
	return task, true, nil
}

// Nothing due is the common case: the sweep asks and goes back to sleep. The
// store answers "no", and no dispatch may happen off the back of it.
func TestQuotaSweepDoesNothingWhenNothingIsDue(t *testing.T) {
	taker := &stubQuotaTaker{}
	s := NewQuotaSweeper(taker, &Dispatcher{})

	s.sweep(context.Background())

	assert.Equal(t, 1, taker.calls, "the store is asked once per pass")
}

// Everything DUE goes in one pass. Unlike the device's single phone there is no
// contention between two tasks whose recorded reset has already passed — they
// became due at the same instant — so holding all but one back only idles them
// for a minute each. The pass ends on the claim that comes back empty, which is
// one extra call.
func TestQuotaSweepDrainsEveryDueTaskInOnePass(t *testing.T) {
	taker := &stubQuotaTaker{tasks: []domain.BoardTask{parkedTask(), parkedTask(), parkedTask()}}
	s := NewQuotaSweeper(taker, &Dispatcher{})

	s.sweep(context.Background())

	assert.Empty(t, taker.tasks, "no due task may be left waiting a whole interval behind another")
	assert.Equal(t, 4, taker.calls, "three claims plus the empty one that ends the pass")
}

// The cap is a brake on the pass, not on the quota: an outage that parked a
// hundred tasks must not dispatch a hundred runs into a three-worker runner in
// one tick. The remainder is claimed on the next pass, a minute later.
func TestQuotaSweepStopsAtTheBatchCap(t *testing.T) {
	parked := make([]domain.BoardTask, quotaSweepBatchCap+5)
	for i := range parked {
		parked[i] = parkedTask()
	}
	taker := &stubQuotaTaker{tasks: parked}
	s := NewQuotaSweeper(taker, &Dispatcher{})

	s.sweep(context.Background())

	assert.Equal(t, quotaSweepBatchCap, taker.calls, "the cap bounds the claims, not just the dispatches")
	assert.Len(t, taker.tasks, 5, "the rest resume on the next pass")
}

// The whole pass shares one "now" — a task must not become due half-way through
// a sweep it was not due at the start of.
func TestQuotaSweepUsesOneClockForTheWholePass(t *testing.T) {
	frozen := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	taker := &stubQuotaTaker{tasks: []domain.BoardTask{parkedTask(), parkedTask()}}
	s := NewQuotaSweeper(taker, &Dispatcher{})
	s.now = func() time.Time { return frozen }

	s.sweep(context.Background())

	require.NotEmpty(t, taker.asked)
	for _, asked := range taker.asked {
		assert.True(t, frozen.Equal(asked))
	}
}

// The resume reason is the quota's own. It said "device_free" while resuming a
// subscription park — the one sentence the history could not have meant, and the
// one the UI has no translation for.
func TestQuotaSweepResumesWithTheQuotaReason(t *testing.T) {
	events := &parkEventStore{}
	dispatcher := NewDispatcher(emptyBoardConfig{}, events, nil, noopEnqueuer{}, true)
	taker := &stubQuotaTaker{tasks: []domain.BoardTask{parkedTask()}}

	NewQuotaSweeper(taker, dispatcher).sweep(context.Background())

	payloads := events.payloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, domain.MoveReasonQuotaRenewed, payloads[0][domain.EventPayloadReason])
	assert.NotEqual(t, domain.MoveReasonResourceFree, payloads[0][domain.EventPayloadReason])
	assert.Equal(t, domain.EventActorSystem, payloads[0][domain.EventPayloadActor])
}

// noopEnqueuer satisfies the dispatcher's runner dependency for a test that only
// cares about the event it writes.
type noopEnqueuer struct{}

func (noopEnqueuer) Enqueue(RunJob) {}

// emptyBoardConfig subscribes no agent to any column, so a dispatch writes its
// event and starts nothing.
type emptyBoardConfig struct{}

func (emptyBoardConfig) GetSettings(context.Context) (domain.BoardSettings, error) {
	return domain.BoardSettings{}, nil
}
func (emptyBoardConfig) UpdateSettings(context.Context, string) (domain.BoardSettings, error) {
	return domain.BoardSettings{}, nil
}
func (emptyBoardConfig) ListColumns(context.Context) ([]domain.BoardColumn, error) { return nil, nil }
func (emptyBoardConfig) ReplaceColumns(context.Context, []domain.BoardColumnInput) error {
	return nil
}
func (emptyBoardConfig) ListMembers(context.Context) ([]domain.BoardMember, error) { return nil, nil }
func (emptyBoardConfig) SetMembers(context.Context, []uuid.UUID) error             { return nil }
func (emptyBoardConfig) ListSubscriptions(context.Context) ([]domain.BoardSubscription, error) {
	return nil, nil
}
func (emptyBoardConfig) SetSubscriptions(context.Context, []domain.BoardSubscriptionInput) error {
	return nil
}
func (emptyBoardConfig) ListAgentSubscriptions(context.Context, uuid.UUID) ([]string, error) {
	return nil, nil
}
func (emptyBoardConfig) SetAgentSubscriptions(context.Context, uuid.UUID, []string) error {
	return nil
}
func (emptyBoardConfig) ListTransitions(context.Context) ([]domain.BoardTransition, error) {
	return nil, nil
}
func (emptyBoardConfig) SetTransitions(context.Context, []domain.BoardTransition) error { return nil }
func (emptyBoardConfig) AgentsForColumn(context.Context, string, string) ([]uuid.UUID, error) {
	return nil, nil
}
func (emptyBoardConfig) ValidateColumnSlug(context.Context, string) (bool, error) { return true, nil }

// The clock is passed IN so one pass cannot see two different "now"s, and so a
// test can drive a reset boundary without sleeping through it.
func TestQuotaSweepAsksWithItsOwnClock(t *testing.T) {
	frozen := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	taker := &stubQuotaTaker{}
	s := NewQuotaSweeper(taker, &Dispatcher{})
	s.now = func() time.Time { return frozen }

	s.sweep(context.Background())

	require.Len(t, taker.asked, 1)
	assert.True(t, frozen.Equal(taker.asked[0]))
}

// A store error must not take the sweeper down: it runs for the life of the
// process and a transient pool failure has to cost one pass, not every future
// one.
func TestQuotaSweepSurvivesAStoreError(t *testing.T) {
	taker := &stubQuotaTaker{err: errors.New("pool closed")}
	s := NewQuotaSweeper(taker, &Dispatcher{})

	assert.NotPanics(t, func() { s.sweep(context.Background()) })
	assert.Equal(t, 1, taker.calls)
}

// Half-built sweepers are a wiring accident, not a runtime state. Start must
// return quietly rather than panic a goroutine minutes later.
func TestQuotaSweeperStartIsSafeWithoutDependencies(t *testing.T) {
	var nilSweeper *QuotaSweeper
	assert.NotPanics(t, func() { nilSweeper.Start(context.Background(), time.Minute) })
	assert.NotPanics(t, func() { NewQuotaSweeper(nil, nil).Start(context.Background(), time.Minute) })
}
