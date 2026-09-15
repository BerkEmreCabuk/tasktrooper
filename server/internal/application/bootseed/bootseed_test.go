package bootseed

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeSeeds stands in for install_state: once the board is seeded it stays
// seeded. calls counts how often the store was asked at all.
type fakeSeeds struct {
	mu      sync.Mutex
	already bool
	seeded  int
	calls   int
	fail    error
}

func (m *fakeSeeds) SeedBoardOnce(context.Context, string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.fail != nil {
		return false, m.fail
	}
	if m.already {
		return false, nil
	}
	m.already = true
	m.seeded++
	return true, nil
}

func (m *fakeSeeds) counts() (seeded, calls int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.seeded, m.calls
}

// stepRecorder counts step runs and signals each one.
type stepRecorder struct {
	mu    sync.Mutex
	runs  int
	fail  error
	after chan struct{}
}

func newStepRecorder() *stepRecorder {
	return &stepRecorder{after: make(chan struct{}, 8)}
}

func (r *stepRecorder) run(context.Context) error {
	r.mu.Lock()
	r.runs++
	err := r.fail
	r.mu.Unlock()
	r.after <- struct{}{}
	return err
}

func (r *stepRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs
}

func (r *stepRecorder) waitFor(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-r.after:
		case <-time.After(3 * time.Second):
			t.Fatalf("only %d of %d step runs happened", i, n)
		}
	}
}

func newService() (*Service, *fakeSeeds) {
	seeds := &fakeSeeds{}
	return NewService(seeds), seeds
}

// Every request calls Ensure, so the steps must run once per process however
// often it is called, and a seeded board must not cost a query per request.
func TestStepsRunOncePerProcess(t *testing.T) {
	svc, seeds := newService()
	rec := newStepRecorder()
	svc.AddStep("probe", rec.run)

	for i := 0; i < 3; i++ {
		if err := svc.Ensure(context.Background()); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
	}
	rec.waitFor(t, 1)
	select {
	case <-rec.after:
		t.Fatal("a step ran a second time in the same process")
	case <-time.After(50 * time.Millisecond):
	}
	if seeded, calls := seeds.counts(); seeded != 1 || calls != 1 {
		t.Fatalf("seeded %d times over %d store calls, want 1 and 1", seeded, calls)
	}
}

// A seed that failed at boot is not cached: the next Ensure (the first request)
// retries it, and the steps wait for a board that exists.
func TestAFailedSeedIsRetried(t *testing.T) {
	svc, seeds := newService()
	rec := newStepRecorder()
	svc.AddStep("probe", rec.run)

	seeds.mu.Lock()
	seeds.fail = errors.New("database not ready")
	seeds.mu.Unlock()
	if err := svc.Ensure(context.Background()); err == nil {
		t.Fatal("a failed seed must surface")
	}
	if svc.Booting() || rec.count() != 0 {
		t.Fatal("steps started before the board was seeded")
	}

	seeds.mu.Lock()
	seeds.fail = nil
	seeds.mu.Unlock()
	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	rec.waitFor(t, 1)
	if seeded, _ := seeds.counts(); seeded != 1 {
		t.Fatalf("seeded %d times, want 1", seeded)
	}
}

// The steps outlive the context that launched them: the request that happened
// to be first is answered long before a dozen seed writes finish.
func TestStepsOutliveTheCallersContext(t *testing.T) {
	svc, _ := newService()
	release := make(chan struct{})
	got := make(chan error, 1)
	svc.AddStep("slow", func(ctx context.Context) error {
		<-release
		got <- ctx.Err()
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.Ensure(ctx); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	cancel()
	close(release)

	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("the step's context ended with its caller's: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the step never ran")
	}
}

// A failing step is logged and skipped, never fatal: an install whose mcp
// catalog did not seed still has a working board, and the caller must not fail
// because of a seed nobody asked for.
func TestAFailingStepDoesNotFailEnsure(t *testing.T) {
	svc, _ := newService()
	bad := newStepRecorder()
	bad.fail = errors.New("nope")
	good := newStepRecorder()
	svc.AddStep("bad", bad.run)
	svc.AddStep("good", good.run)

	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure must not surface a step failure: %v", err)
	}
	bad.waitFor(t, 1)
	good.waitFor(t, 1)
}

// The board seed is the durable, once-ever half; the steps are the
// once-per-process half. An install seeded by an earlier build still gets the
// steps, which is how new seed data reaches it.
func TestStepsRunForAnAlreadySeededInstall(t *testing.T) {
	svc, seeds := newService()
	rec := newStepRecorder()
	svc.AddStep("probe", rec.run)

	seeds.already = true // install_state says the board already exists

	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	rec.waitFor(t, 1)

	if seeded, _ := seeds.counts(); seeded != 0 {
		t.Fatalf("the board was re-seeded %d times on an install that already had one", seeded)
	}
	if n := rec.count(); n != 1 {
		t.Fatalf("step ran %d times on an existing install, want 1", n)
	}
}
