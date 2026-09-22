package board

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type sharedRunStore struct {
	mu     sync.Mutex
	rows   map[uuid.UUID]domain.TaskAgentRun
	claims int
}

func newSharedRunStore() *sharedRunStore {
	return &sharedRunStore{
		rows: map[uuid.UUID]domain.TaskAgentRun{},
	}
}

func (s *sharedRunStore) put(run domain.TaskAgentRun) domain.TaskAgentRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.Status == "" {
		run.Status = domain.TaskAgentRunStatusPending
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = time.Now()
	}
	s.rows[run.ID] = run
	return run
}

func (s *sharedRunStore) get(id uuid.UUID) domain.TaskAgentRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[id]
}

func (s *sharedRunStore) claimCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claims
}

func (s *sharedRunStore) ClaimRun(_ context.Context, c port.RunClaim) (port.RunClaimResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	me, ok := s.rows[c.RunID]
	if !ok || me.Status != domain.TaskAgentRunStatusPending {
		return port.RunClaimResult{Reason: "not_pending"}, nil
	}
	for _, row := range s.rows {
		if row.Status != domain.TaskAgentRunStatusRunning {
			continue
		}
		if time.Since(row.UpdatedAt) > c.LiveWithin {
			continue
		}
		if row.TaskID == me.TaskID {
			return port.RunClaimResult{Reason: "task_busy"}, nil
		}
	}
	me.Status = domain.TaskAgentRunStatusRunning
	me.UpdatedAt = time.Now()
	s.rows[c.RunID] = me
	s.claims++
	return port.RunClaimResult{Claimed: true}, nil
}

func (s *sharedRunStore) Touch(_ context.Context, id uuid.UUID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return "", nil
	}
	if row.Status == domain.TaskAgentRunStatusPending || row.Status == domain.TaskAgentRunStatusRunning {
		row.UpdatedAt = time.Now()
		s.rows[id] = row
	}
	return row.Status, nil
}

func (s *sharedRunStore) CancelIfLive(_ context.Context, id uuid.UUID, reason string) (domain.TaskAgentRun, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || (row.Status != domain.TaskAgentRunStatusPending && row.Status != domain.TaskAgentRunStatusRunning) {
		return domain.TaskAgentRun{}, false, nil
	}
	row.Status = domain.TaskAgentRunStatusCancelled
	row.Summary = reason
	row.UpdatedAt = time.Now()
	s.rows[id] = row
	return row, true, nil
}

func (s *sharedRunStore) FailIfStale(_ context.Context, id uuid.UUID, cutoff time.Time, summary string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || (row.Status != domain.TaskAgentRunStatusPending && row.Status != domain.TaskAgentRunStatusRunning) {
		return false, nil
	}
	if !row.UpdatedAt.Before(cutoff) {
		return false, nil
	}
	row.Status = domain.TaskAgentRunStatusFailed
	row.Summary = summary
	s.rows[id] = row
	return true, nil
}

func (s *sharedRunStore) HasLiveRunForTask(_ context.Context, taskID uuid.UUID, within time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		if row.TaskID == taskID && row.Status == domain.TaskAgentRunStatusRunning && time.Since(row.UpdatedAt) <= within {
			return true, nil
		}
	}
	return false, nil
}

func (s *sharedRunStore) Create(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	return s.put(run), nil
}

func (s *sharedRunStore) Update(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[run.ID]
	if ok && current.Status == domain.TaskAgentRunStatusCancelled {
		run.Status = domain.TaskAgentRunStatusCancelled
	}
	run.UpdatedAt = time.Now()
	s.rows[run.ID] = run
	return run, nil
}

func (s *sharedRunStore) GetByID(_ context.Context, id uuid.UUID) (domain.TaskAgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rows[id], nil
}

func (s *sharedRunStore) ListByTask(context.Context, uuid.UUID, int) ([]domain.TaskAgentRun, error) {
	return nil, nil
}
func (s *sharedRunStore) ListRecent(context.Context, int) ([]domain.TaskAgentRun, error) {
	return nil, nil
}
func (s *sharedRunStore) HasPendingForEvent(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *sharedRunStore) HasPendingForTask(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *sharedRunStore) HasLiveForTask(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *sharedRunStore) ListStale(context.Context, time.Time) ([]domain.TaskAgentRun, error) {
	return nil, nil
}

type countingCatalog struct {
	port.CatalogStore
	mu      sync.Mutex
	entered int
	release chan struct{}
}

func newCountingCatalog() *countingCatalog {
	return &countingCatalog{release: make(chan struct{})}
}

func (c *countingCatalog) GetAgent(ctx context.Context, _ uuid.UUID) (domain.Agent, error) {
	c.mu.Lock()
	c.entered++
	c.mu.Unlock()
	select {
	case <-c.release:
	case <-ctx.Done():
	}
	return domain.Agent{}, context.Canceled
}

func (c *countingCatalog) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entered
}

func waitUntil(cond func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestTwoReplicasCannotRunTheSameTask(t *testing.T) {
	store := newSharedRunStore()
	catalog := newCountingCatalog()
	defer close(catalog.release)

	taskID := uuid.New()
	runA := store.put(domain.TaskAgentRun{TaskID: taskID})
	runB := store.put(domain.TaskAgentRun{TaskID: taskID})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := NewRunner(RunnerDeps{Runs: store, Catalog: catalog})
	b := NewRunner(RunnerDeps{Runs: store, Catalog: catalog})
	a.Start(ctx)
	b.Start(ctx)
	defer a.Stop()
	defer b.Stop()

	task := domain.BoardTask{ID: taskID}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.Enqueue(RunJob{Run: runA, Task: task}) }()
	go func() { defer wg.Done(); b.Enqueue(RunJob{Run: runB, Task: task}) }()
	wg.Wait()

	if !waitUntil(func() bool { return store.claimCount() >= 1 }) {
		t.Fatal("neither replica claimed the run")
	}

	time.Sleep(150 * time.Millisecond)

	if got := store.claimCount(); got != 1 {
		t.Fatalf("%d replicas claimed the same task, want 1", got)
	}
	if got := catalog.count(); got != 1 {
		t.Fatalf("%d agents started on one task, want 1", got)
	}

	pendingCount := 0
	for _, id := range []uuid.UUID{runA.ID, runB.ID} {
		if store.get(id).Status == domain.TaskAgentRunStatusPending {
			pendingCount++
		}
	}
	if pendingCount != 1 {
		t.Fatalf("%d runs left pending, want the loser to be exactly one", pendingCount)
	}
}

func TestRunnerStartsEveryRunWithoutACap(t *testing.T) {
	store := newSharedRunStore()
	catalog := newCountingCatalog()
	defer close(catalog.release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := NewRunner(RunnerDeps{Runs: store, Catalog: catalog})
	r.Start(ctx)
	defer r.Stop()

	const runs = 12
	for i := 0; i < runs; i++ {
		taskID := uuid.New()
		run := store.put(domain.TaskAgentRun{TaskID: taskID})
		r.Enqueue(RunJob{Run: run, Task: domain.BoardTask{ID: taskID}})
	}

	if !waitUntil(func() bool { return store.claimCount() == runs }) {
		t.Fatalf("%d of %d runs started, want all of them at once", store.claimCount(), runs)
	}
}

func TestStopOnAnotherReplicaStopsTheRun(t *testing.T) {
	store := newSharedRunStore()
	catalog := newCountingCatalog()
	defer close(catalog.release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	executing := NewRunner(RunnerDeps{Runs: store, Catalog: catalog})
	executing.heartbeatEvery = 10 * time.Millisecond
	executing.Start(ctx)
	defer executing.Stop()

	other := NewRunner(RunnerDeps{Runs: store, Catalog: catalog})
	other.Start(ctx)
	defer other.Stop()

	taskID := uuid.New()
	run := store.put(domain.TaskAgentRun{TaskID: taskID})
	executing.Enqueue(RunJob{Run: run, Task: domain.BoardTask{ID: taskID}})

	if !waitUntil(func() bool { return catalog.count() == 1 }) {
		t.Fatal("the run never started")
	}
	if executing.IsActive(run.ID) != true {
		t.Fatal("the run should be executing on the first replica")
	}
	if other.Cancel(run.ID) {
		t.Fatal("the other replica must not claim to hold a run it never started")
	}

	if _, ok, err := store.CancelIfLive(context.Background(), run.ID, "user stopped"); err != nil || !ok {
		t.Fatalf("CancelIfLive: ok=%v err=%v", ok, err)
	}

	if !waitUntil(func() bool { return !executing.IsActive(run.ID) }) {
		t.Fatal("the run kept going after it was cancelled from another replica")
	}
	if got := store.get(run.ID).Status; got != domain.TaskAgentRunStatusCancelled {
		t.Fatalf("row status = %q, want cancelled", got)
	}
}

type countingQA struct {
	mu    sync.Mutex
	calls int
}

func (q *countingQA) DispatchQA(context.Context, uuid.UUID, domain.BoardTask, uuid.UUID, string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls++
	return nil
}

func (q *countingQA) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.calls
}
func TestOnlyOneReplicaFinalizesAPipeline(t *testing.T) {
	store := newFakePipelineStore()
	qa := &countingQA{}

	a := NewPipelineRunner(PipelineRunnerDeps{Store: store, QA: qa})
	b := NewPipelineRunner(PipelineRunnerDeps{Store: store, QA: qa})

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New()}
	pipeline, err := store.Create(context.Background(), domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID, Trigger: domain.PipelineTriggerReadyForQA,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	job := pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, runner := range []*PipelineRunner{a, b} {
		wg.Add(1)
		go func(r *PipelineRunner) {
			defer wg.Done()
			<-start
			_ = r.finalize(context.Background(), job, pipeline, domain.PipelineStatusSuccess, nil)
		}(runner)
	}
	close(start)
	wg.Wait()

	if got := qa.count(); got != 1 {
		t.Fatalf("%d QA hand-offs for one pipeline, want 1", got)
	}
	stored, err := store.Get(context.Background(), pipeline.ID)
	if err != nil {
		t.Fatalf("get pipeline: %v", err)
	}
	if stored.Status != domain.PipelineStatusSuccess {
		t.Fatalf("status = %q, want success", stored.Status)
	}
}

func (c *countingCatalog) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
