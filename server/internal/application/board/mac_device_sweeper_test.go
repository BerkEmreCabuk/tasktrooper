package board

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubMemberProbe struct {
	mu    sync.Mutex
	free  map[string]bool
	calls map[string]int
}

func (s *stubMemberProbe) ProbeMember(_ context.Context, member string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.calls[member]++
	return s.free[member]
}

func TestMacDeviceSweepProbesOncePerMember(t *testing.T) {
	a1, a2, a3 := runnerParkedTask("alice"), runnerParkedTask("alice"), runnerParkedTask("alice")
	b1 := runnerParkedTask("bob")
	store := &stubRunnerParkStore{parked: []domain.BoardTask{a1, a2, a3, b1}}
	probe := &stubMemberProbe{free: map[string]bool{"alice": true, "bob": false}}

	s := NewMacDeviceSweeper(store, probe, &Dispatcher{})
	require.NotNil(t, s)
	s.sweep(runnerSweepCtx())

	assert.Equal(t, 1, probe.calls["alice"])
	assert.Equal(t, 1, probe.calls["bob"])
	assert.Len(t, store.takenIDs, 1)
	assert.NotContains(t, store.takenIDs, b1.ID)
}

func TestMacDeviceSweepLeavesABusyMacParked(t *testing.T) {
	task := runnerParkedTask("alice")
	store := &stubRunnerParkStore{parked: []domain.BoardTask{task}}
	probe := &stubMemberProbe{free: map[string]bool{"alice": false}}

	NewMacDeviceSweeper(store, probe, &Dispatcher{}).sweep(runnerSweepCtx())

	assert.Empty(t, store.takenIDs)
}

func TestMacDeviceSweepSkipsAnUnassignedCard(t *testing.T) {
	orphan := runnerParkedTask("")
	store := &stubRunnerParkStore{parked: []domain.BoardTask{orphan}}
	probe := &stubMemberProbe{free: map[string]bool{}}

	NewMacDeviceSweeper(store, probe, &Dispatcher{}).sweep(runnerSweepCtx())

	assert.Empty(t, store.takenIDs)
	assert.Empty(t, probe.calls, "there is no Mac to ask about")
}

func TestMacDeviceSweepToleratesALostRace(t *testing.T) {
	a1, a2 := runnerParkedTask("alice"), runnerParkedTask("bob")
	store := &stubRunnerParkStore{
		parked:     []domain.BoardTask{a1, a2},
		takeMisses: map[uuid.UUID]bool{a1.ID: true},
	}
	probe := &stubMemberProbe{free: map[string]bool{"alice": true, "bob": true}}

	NewMacDeviceSweeper(store, probe, &Dispatcher{}).sweep(runnerSweepCtx())

	assert.Equal(t, 1, probe.calls["bob"], "the pass carried on to the next member")
}

func TestMacDeviceSweepAsksNothingWithNothingParked(t *testing.T) {
	store := &stubRunnerParkStore{}
	probe := &stubMemberProbe{}

	NewMacDeviceSweeper(store, probe, &Dispatcher{}).sweep(runnerSweepCtx())

	assert.Empty(t, probe.calls)
}

func TestMacDeviceSweeperIsNilWithoutItsParts(t *testing.T) {
	assert.Nil(t, NewMacDeviceSweeper(nil, &stubMemberProbe{}, &Dispatcher{}))
	assert.Nil(t, NewMacDeviceSweeper(&stubRunnerParkStore{}, nil, &Dispatcher{}))
	assert.Nil(t, NewMacDeviceSweeper(&stubRunnerParkStore{}, &stubMemberProbe{}, nil))
}
