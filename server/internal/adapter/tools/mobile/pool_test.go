package mobile

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// poolOf builds a pool over its own hubs, one per phone, so a test can make one
// device busy without making the installation busy.
func poolOf(t *testing.T, udids ...string) (*Pool, []*hub) {
	t.Helper()
	p := NewPool()
	cfgs := make([]Config, 0, len(udids))
	hubs := make([]*hub, 0, len(udids))
	for _, udid := range udids {
		h := newHub()
		srv := h.serve(t)
		cfgs = append(cfgs, Config{HubURL: srv.URL, DeviceUDID: udid})
		hubs = append(hubs, h)
	}
	p.Reconfigure(cfgs)
	t.Cleanup(p.Close)
	return p, hubs
}

func runCtx(t *testing.T) context.Context {
	t.Helper()
	return registry.ContextWithSessionID(context.Background(), uuid.New())
}

// The lease is per phone, which is the whole point: a run that finds the first
// device taken is handed the second rather than being parked.
func TestAcquireSkipsATakenDevice(t *testing.T) {
	p, _ := poolOf(t, "127.0.0.1:5555", "127.0.0.1:5556")

	first := runCtx(t)
	one, err := p.acquire(first)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:5555", one.udid())

	second := runCtx(t)
	two, err := p.acquire(second)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:5556", two.udid(),
		"the second run was handed the phone the first one is driving")
}

// Every call in one run has to reach the same phone, or launch-then-tap-then-
// screenshot describes three different devices.
func TestAcquireIsStableWithinARun(t *testing.T) {
	p, _ := poolOf(t, "127.0.0.1:5555", "127.0.0.1:5556")

	run := runCtx(t)
	first, err := p.acquire(run)
	require.NoError(t, err)
	again, err := p.acquire(run)
	require.NoError(t, err)
	assert.Same(t, first, again)
}

// Parking is only correct when EVERY phone is taken. Reporting busy while one
// is free would put a task in the blocked column to wait ten minutes for a
// device that was available the whole time.
func TestAcquireReportsBusyOnlyWhenAllDevicesAreTaken(t *testing.T) {
	p, _ := poolOf(t, "127.0.0.1:5555", "127.0.0.1:5556")

	_, err := p.acquire(runCtx(t))
	require.NoError(t, err)
	_, err = p.acquire(runCtx(t))
	require.NoError(t, err)

	_, err = p.acquire(runCtx(t))
	assert.True(t, errors.Is(err, errDeviceBusy), "third run got %v, want the busy signal", err)
}

// A released phone goes back to the pool immediately: the next run must not
// wait for Appium's own idle reaping to have a device again.
func TestReleaseReturnsThePhoneToThePool(t *testing.T) {
	p, _ := poolOf(t, "127.0.0.1:5555")

	holder := runCtx(t)
	_, err := p.acquire(holder)
	require.NoError(t, err)
	_, err = p.acquire(runCtx(t))
	assert.True(t, errors.Is(err, errDeviceBusy))

	p.Release(holder)
	got, err := p.acquire(runCtx(t))
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:5555", got.udid())
}

// Reconfigure is what a settings change runs through, and the phone that did
// not change must not lose the lease a run is in the middle of using.
func TestReconfigureKeepsALiveLeaseOnAnUnchangedPhone(t *testing.T) {
	p, _ := poolOf(t, "127.0.0.1:5555")

	run := runCtx(t)
	held, err := p.acquire(run)
	require.NoError(t, err)

	added := newHub()
	srv := added.serve(t)
	cfgs := []Config{
		{HubURL: held.cfg.HubURL, DeviceUDID: "127.0.0.1:5555"},
		{HubURL: srv.URL, DeviceUDID: "127.0.0.1:5556"},
	}
	p.Reconfigure(cfgs)

	assert.True(t, p.Busy("127.0.0.1:5555"), "the untouched phone lost its lease when a second one was registered")
	again, err := p.acquire(run)
	require.NoError(t, err)
	assert.Same(t, held, again)
}

// The hub is shared between phones, so a live session on one of them says
// nothing about the other. Reading it as "the device is busy" is what parked
// every task the moment any phone was taken.
func TestProbeIgnoresASessionOnAnotherPhone(t *testing.T) {
	h := newHub()
	h.handler["GET /sessions"] = func() (int, string) {
		return 200, `{"value":[{"id":"abc","capabilities":{"appium:udid":"127.0.0.1:5555"}}]}`
	}
	srv := h.serve(t)
	free := NewSession(Config{HubURL: srv.URL, DeviceUDID: "127.0.0.1:5556"})
	t.Cleanup(free.Close)
	taken := NewSession(Config{HubURL: srv.URL, DeviceUDID: "127.0.0.1:5555"})
	t.Cleanup(taken.Close)

	assert.True(t, free.Probe(context.Background()), "a phone with no session of its own reported busy")
	assert.False(t, taken.Probe(context.Background()), "the phone the hub has a session for reported free")
}

// The sweeper asks one question — could a parked task run now — and one free
// phone is enough to answer it.
func TestProbeIsTrueWhileAnyPhoneIsFree(t *testing.T) {
	p, _ := poolOf(t, "127.0.0.1:5555", "127.0.0.1:5556")

	_, err := p.acquire(runCtx(t))
	require.NoError(t, err)
	assert.True(t, p.Probe(context.Background()), "one phone is held and the other is free")

	_, err = p.acquire(runCtx(t))
	require.NoError(t, err)
	assert.False(t, p.Probe(context.Background()), "both phones are held")
}
