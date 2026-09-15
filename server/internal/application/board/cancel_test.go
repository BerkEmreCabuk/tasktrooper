package board

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type gatedRunStore struct {
	countingRunStore
	row domain.TaskAgentRun
	err error
}

func (g *gatedRunStore) GetByID(_ context.Context, _ uuid.UUID) (domain.TaskAgentRun, error) {
	return g.row, g.err
}

type blockingCatalog struct {
	port.CatalogStore
	entered chan struct{}
}

func (c *blockingCatalog) GetAgent(ctx context.Context, _ uuid.UUID) (domain.Agent, error) {
	close(c.entered)
	<-ctx.Done()
	return domain.Agent{}, ctx.Err()
}

type erroringCatalog struct {
	port.CatalogStore
}

func (c *erroringCatalog) GetAgent(context.Context, uuid.UUID) (domain.Agent, error) {
	return domain.Agent{}, errors.New("no such agent")
}

type commentRecorder struct {
	mu       sync.Mutex
	comments []domain.CreateTaskCommentRequest
}

func (c *commentRecorder) UpdateTask(context.Context, uuid.UUID, uuid.UUID, domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}

func (c *commentRecorder) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.comments = append(c.comments, req)
	return domain.TaskComment{}, nil
}

func (c *commentRecorder) ListComments(context.Context, uuid.UUID, uuid.UUID) ([]domain.TaskComment, error) {
	return nil, nil
}

func (c *commentRecorder) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.comments)
}

func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func TestWorkerSkipsRunsThatAlreadyStopped(t *testing.T) {
	tests := []struct {
		name    string
		row     domain.TaskAgentRun
		err     error
		wantRun bool
	}{
		{
			name: "a cancelled run is not started",
			row:  domain.TaskAgentRun{Status: domain.TaskAgentRunStatusCancelled},
		},
		{
			name: "a run that already finished is not started again",
			row:  domain.TaskAgentRun{Status: domain.TaskAgentRunStatusCompleted},
		},
		{
			name:    "a pending run is started",
			row:     domain.TaskAgentRun{Status: domain.TaskAgentRunStatusPending},
			wantRun: true,
		},
		{
			name:    "a row that cannot be read is started anyway",
			err:     errors.New("pool closed"),
			wantRun: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &gatedRunStore{row: tc.row, err: tc.err}
			r := NewRunner(RunnerDeps{Runs: store, Catalog: &erroringCatalog{}})
			r.Start(context.Background())
			defer r.Stop()

			taskID := uuid.New()
			r.Enqueue(RunJob{
				Run:  domain.TaskAgentRun{ID: uuid.New(), TaskID: taskID},
				Task: domain.BoardTask{ID: taskID},
			})

			ran := waitFor(func() bool { return store.updates.Load() > 0 })
			if ran != tc.wantRun {
				t.Fatalf("run started = %v, want %v", ran, tc.wantRun)
			}
			if !tc.wantRun && !r.beginTask(jobFor(taskID)) {
				t.Fatal("a skipped job must not have claimed the task")
			}
		})
	}
}

func TestCancelStopsTheInFlightRunsContext(t *testing.T) {
	store := &gatedRunStore{row: domain.TaskAgentRun{Status: domain.TaskAgentRunStatusPending}}
	catalog := &blockingCatalog{entered: make(chan struct{})}
	r := NewRunner(RunnerDeps{Runs: store, Catalog: catalog})
	r.Start(context.Background())
	defer r.Stop()

	runID := uuid.New()
	taskID := uuid.New()
	r.Enqueue(RunJob{Run: domain.TaskAgentRun{ID: runID, TaskID: taskID}, Task: domain.BoardTask{ID: taskID}})

	select {
	case <-catalog.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the run never started")
	}

	if !r.Cancel(runID) {
		t.Fatal("Cancel did not find a run this process is executing")
	}
	if !waitFor(func() bool { return !r.IsActive(runID) }) {
		t.Fatal("the run kept going after its context was cancelled")
	}
}

func TestCancelReportsFalseForRunsItDoesNotHold(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})

	if r.Cancel(uuid.New()) {
		t.Fatal("Cancel claimed a run this process never executed")
	}

	runID := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Register and release, exactly as a run that has just finished does.
	r.registerCancel(runID, cancel)()

	if r.Cancel(runID) {
		t.Fatal("Cancel claimed a run that had already ended")
	}
	if ctx.Err() != nil {
		t.Fatal("Cancel cancelled a context the runner no longer holds")
	}
}

func TestStoppedRunWritesNoFailure(t *testing.T) {
	store := &gatedRunStore{row: domain.TaskAgentRun{Status: domain.TaskAgentRunStatusPending}}
	catalog := &blockingCatalog{entered: make(chan struct{})}
	comments := &commentRecorder{}
	r := NewRunner(RunnerDeps{Runs: store, Catalog: catalog})
	r.SetTaskUpdater(comments)
	r.Start(context.Background())
	defer r.Stop()

	runID := uuid.New()
	taskID := uuid.New()
	r.Enqueue(RunJob{Run: domain.TaskAgentRun{ID: runID, TaskID: taskID}, Task: domain.BoardTask{ID: taskID}})

	select {
	case <-catalog.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the run never started")
	}
	if got := store.claims.Load(); got != 1 {
		t.Fatalf("claims before the stop = %d, want 1", got)
	}
	if got := store.updates.Load(); got != 0 {
		t.Fatalf("writes before the stop = %d, want 0", got)
	}

	r.Cancel(runID)
	if !waitFor(func() bool { return !r.IsActive(runID) }) {
		t.Fatal("the run kept going after it was stopped")
	}

	if got := store.updates.Load(); got != 0 {
		t.Fatalf("a stopped run wrote %d times, want no write after the stop", got)
	}
	if got := comments.count(); got != 0 {
		t.Fatalf("a stopped run left %d comments on the task, want none", got)
	}
}

func (c *blockingCatalog) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}

func (c *erroringCatalog) ListTechStacksByAgent(context.Context, uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
