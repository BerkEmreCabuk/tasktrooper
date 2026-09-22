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

func TestQuotaSweepDoesNothingWhenNothingIsDue(t *testing.T) {
	taker := &stubQuotaTaker{}
	s := NewQuotaSweeper(taker, &Dispatcher{})

	s.sweep(context.Background())

	assert.Equal(t, 1, taker.calls, "the store is asked once per pass")
}

func TestQuotaSweepDrainsEveryDueTaskInOnePass(t *testing.T) {
	taker := &stubQuotaTaker{tasks: []domain.BoardTask{parkedTask(), parkedTask(), parkedTask()}}
	s := NewQuotaSweeper(taker, &Dispatcher{})

	s.sweep(context.Background())

	assert.Empty(t, taker.tasks, "no due task may be left waiting a whole interval behind another")
	assert.Equal(t, 4, taker.calls, "three claims plus the empty one that ends the pass")
}

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

type noopEnqueuer struct{}

func (noopEnqueuer) Enqueue(RunJob) {}

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
func (emptyBoardConfig) ListAgentSubscriptionsDetailed(context.Context, uuid.UUID) ([]domain.AgentColumnSubscription, error) {
	return nil, nil
}
func (emptyBoardConfig) SetAgentSubscriptionsDetailed(context.Context, uuid.UUID, []domain.AgentColumnSubscription) error {
	return nil
}
func (emptyBoardConfig) ListAgentColumnInstructions(context.Context, uuid.UUID) ([]domain.AgentColumnInstruction, error) {
	return nil, nil
}
func (emptyBoardConfig) SetAgentColumnInstruction(context.Context, uuid.UUID, string, string) error {
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

func TestQuotaSweepAsksWithItsOwnClock(t *testing.T) {
	frozen := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	taker := &stubQuotaTaker{}
	s := NewQuotaSweeper(taker, &Dispatcher{})
	s.now = func() time.Time { return frozen }

	s.sweep(context.Background())

	require.Len(t, taker.asked, 1)
	assert.True(t, frozen.Equal(taker.asked[0]))
}

func TestQuotaSweepSurvivesAStoreError(t *testing.T) {
	taker := &stubQuotaTaker{err: errors.New("pool closed")}
	s := NewQuotaSweeper(taker, &Dispatcher{})

	assert.NotPanics(t, func() { s.sweep(context.Background()) })
	assert.Equal(t, 1, taker.calls)
}

func TestQuotaSweeperStartIsSafeWithoutDependencies(t *testing.T) {
	var nilSweeper *QuotaSweeper
	assert.NotPanics(t, func() { nilSweeper.Start(context.Background(), time.Minute) })
	assert.NotPanics(t, func() { NewQuotaSweeper(nil, nil).Start(context.Background(), time.Minute) })
}
