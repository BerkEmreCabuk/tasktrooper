package board

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type countingRunStore struct {
	updates atomic.Int32
	touches atomic.Int32
	claims  atomic.Int32
}

func (c *countingRunStore) Create(context.Context, domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	return domain.TaskAgentRun{}, nil
}

func (c *countingRunStore) Update(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	c.updates.Add(1)
	return run, nil
}

func (c *countingRunStore) ListByTask(context.Context, uuid.UUID, int) ([]domain.TaskAgentRun, error) {
	return nil, nil
}

func (c *countingRunStore) ListRecent(context.Context, int) ([]domain.TaskAgentRun, error) {
	return nil, nil
}

func (c *countingRunStore) HasPendingForEvent(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (c *countingRunStore) HasPendingForTask(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (c *countingRunStore) HasLiveForTask(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (c *countingRunStore) ListStale(context.Context, time.Time) ([]domain.TaskAgentRun, error) {
	return nil, nil
}

func (c *countingRunStore) Touch(context.Context, uuid.UUID) (string, error) {
	c.touches.Add(1)
	return domain.TaskAgentRunStatusRunning, nil
}

func (c *countingRunStore) ClaimRun(context.Context, port.RunClaim) (port.RunClaimResult, error) {
	c.claims.Add(1)
	return port.RunClaimResult{Claimed: true}, nil
}

func (c *countingRunStore) FailIfStale(context.Context, uuid.UUID, time.Time, string) (bool, error) {
	return true, nil
}

func (c *countingRunStore) HasLiveRunForTask(context.Context, uuid.UUID, time.Duration) (bool, error) {
	return false, nil
}

func (c *countingRunStore) GetByID(_ context.Context, id uuid.UUID) (domain.TaskAgentRun, error) {
	return domain.TaskAgentRun{ID: id, Status: domain.TaskAgentRunStatusPending}, nil
}

func (c *countingRunStore) CancelIfLive(_ context.Context, id uuid.UUID, reason string) (domain.TaskAgentRun, bool, error) {
	return domain.TaskAgentRun{ID: id, Status: domain.TaskAgentRunStatusCancelled, Summary: reason}, true, nil
}

func TestDrainStopsAcceptingNewJobs(t *testing.T) {
	store := &countingRunStore{}
	r := NewRunner(RunnerDeps{Runs: store, MaxWorkers: 2})
	r.Start(context.Background())

	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.Drain(drainCtx)

	r.Enqueue(RunJob{Run: domain.TaskAgentRun{ID: uuid.New()}})
	time.Sleep(50 * time.Millisecond)

	if got := store.updates.Load(); got != 0 {
		t.Fatalf("runs picked up after drain = %d, want 0", got)
	}
}

func TestDrainWaitsForInFlightWork(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}, MaxWorkers: 1})

	var wg sync.WaitGroup
	wg.Add(1)
	r.wg.Add(1)
	finished := make(chan struct{})
	go func() {
		defer r.wg.Done()
		wg.Done()
		time.Sleep(150 * time.Millisecond)
		close(finished)
	}()
	wg.Wait()

	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.Drain(drainCtx)

	select {
	case <-finished:
	default:
		t.Fatal("Drain returned while work was still in flight")
	}
}

func TestDrainDeadlineCancelsInFlightWork(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}, MaxWorkers: 1})
	ctx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	r.Start(ctx)

	runCtx := make(chan context.Context, 1)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		<-runCtx
	}()

	drainCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		<-drainCtx.Done()
		runCtx <- context.Background()
		close(done)
	}()

	r.Drain(drainCtx)
	<-done
}

func TestActiveRunRegistry(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	id := uuid.New()

	if r.IsActive(id) {
		t.Fatal("run is not running yet")
	}
	release := r.markActive(id)
	if !r.IsActive(id) || r.ActiveCount() != 1 {
		t.Fatalf("run must be reported active, count = %d", r.ActiveCount())
	}
	release()
	if r.IsActive(id) || r.ActiveCount() != 0 {
		t.Fatal("finished run must not stay in the active set")
	}
}
