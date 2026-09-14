package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubProbe struct{ free bool }

func (p stubProbe) Probe(context.Context) bool { return p.free }

type stubResourceTaker struct {
	tasks []domain.BoardTask
	calls int
	err   error
}

func (s *stubResourceTaker) TakeBlockedByResource(context.Context, string) (domain.BoardTask, bool, error) {
	s.calls++
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

func parkedTask() domain.BoardTask {
	return domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: uuid.New(),
		Column:       domain.TaskColumnInQA,
	}
}

func TestSweepDoesNothingWhileTheDeviceIsHeld(t *testing.T) {
	taker := &stubResourceTaker{tasks: []domain.BoardTask{parkedTask()}}
	s := NewDeviceSweeper(taker, stubProbe{free: false}, &Dispatcher{})

	s.sweep(context.Background())

	assert.Zero(t, taker.calls, "no task may be claimed while the device is busy")
}

func TestSweepResumesOnlyOneTaskPerPass(t *testing.T) {
	taker := &stubResourceTaker{tasks: []domain.BoardTask{parkedTask(), parkedTask()}}
	s := NewDeviceSweeper(taker, stubProbe{free: true}, &Dispatcher{})

	s.sweep(context.Background())

	assert.Equal(t, 1, taker.calls)
	assert.Len(t, taker.tasks, 1, "the second task stays parked for the next pass")
}

func TestSweepIsANoOpWithNothingParked(t *testing.T) {
	taker := &stubResourceTaker{}
	s := NewDeviceSweeper(taker, stubProbe{free: true}, &Dispatcher{})

	s.sweep(context.Background())

	assert.Equal(t, 1, taker.calls)
}

func TestSweepSurvivesAClaimFailure(t *testing.T) {
	taker := &stubResourceTaker{err: errors.New("db down")}
	s := NewDeviceSweeper(taker, stubProbe{free: true}, &Dispatcher{})

	require.NotPanics(t, func() { s.sweep(context.Background()) })
}

func TestSweeperWithMissingDependenciesDoesNotStart(t *testing.T) {
	require.NotPanics(t, func() {
		NewDeviceSweeper(nil, nil, nil).Start(context.Background(), 0)
		var nilSweeper *DeviceSweeper
		nilSweeper.Start(context.Background(), 0)
	})
}
