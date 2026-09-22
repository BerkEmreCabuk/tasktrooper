package board

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type limitSettings struct {
	agents, tasks int
}

func (l *limitSettings) Get(context.Context) (domain.AppSettings, error) {
	return domain.AppSettings{MaxConcurrentAgents: l.agents, MaxConcurrentTasks: l.tasks}, nil
}

func (l *limitSettings) Update(context.Context, domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	return domain.AppSettings{}, nil
}

type entryCountingCatalog struct {
	port.CatalogStore
	entries atomic.Int32
	first   atomic.Value
	seen    atomic.Value
}

func (c *entryCountingCatalog) GetAgent(ctx context.Context, agentID uuid.UUID) (domain.Agent, error) {
	c.entries.Add(1)
	if c.first.Load() == nil {
		c.first.Store(agentID)
	}
	c.seen.Store(agentID)
	defer c.entries.Add(-1)
	<-ctx.Done()
	return domain.Agent{}, ctx.Err()
}

func (c *entryCountingCatalog) runningAgentID() uuid.UUID {
	if id, ok := c.first.Load().(uuid.UUID); ok {
		return id
	}
	return uuid.Nil
}

func TestAgentSlotCapsConcurrentRuns(t *testing.T) {
	cat := &entryCountingCatalog{}
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}, Catalog: cat, Settings: &limitSettings{agents: 1}})
	r.Start(context.Background())
	defer r.Stop()

	run1, run2 := uuid.New(), uuid.New()
	r.Enqueue(RunJob{Run: domain.TaskAgentRun{ID: run1, TaskID: uuid.New(), AgentID: run1}, Task: domain.BoardTask{ID: uuid.New()}})
	r.Enqueue(RunJob{Run: domain.TaskAgentRun{ID: run2, TaskID: uuid.New(), AgentID: run2}, Task: domain.BoardTask{ID: uuid.New()}})

	if !waitFor(func() bool { return cat.entries.Load() >= 1 }) {
		t.Fatal("first run never entered the agent loop")
	}
	time.Sleep(75 * time.Millisecond)
	if got := cat.entries.Load(); got != 1 {
		t.Fatalf("ran %d runs at once under a cap of one", got)
	}

	active := cat.runningAgentID()
	if active == uuid.Nil || !r.Cancel(active) {
		t.Fatal("cancel did not reach the active run")
	}
	if !waitFor(func() bool {
		seen, _ := cat.seen.Load().(uuid.UUID)
		return seen != uuid.Nil && seen != active
	}) {
		t.Fatal("queued run never entered the agent loop after the cap freed")
	}
}

func TestNoAgentCapLetsRunsRunConcurrently(t *testing.T) {
	cat := &entryCountingCatalog{}
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}, Catalog: cat})
	r.Start(context.Background())
	defer r.Stop()

	for i := 0; i < 2; i++ {
		r.Enqueue(RunJob{Run: domain.TaskAgentRun{ID: uuid.New(), TaskID: uuid.New()}, Task: domain.BoardTask{ID: uuid.New()}})
	}

	if !waitFor(func() bool { return cat.entries.Load() >= 2 }) {
		t.Fatal("uncapped runs never reached concurrency two")
	}
}

func TestTaskSlotCapsDistinctTasks(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	r.taskSlots.setLimit(1)

	a := uuid.New()
	if !r.beginTask(context.Background(), jobFor(a)) {
		t.Fatal("first task must start")
	}

	claimed := make(chan bool, 1)
	go func() { claimed <- r.beginTask(context.Background(), jobFor(uuid.New())) }()
	select {
	case <-claimed:
		t.Fatal("second distinct task claimed while the cap is reached")
	case <-time.After(150 * time.Millisecond):
	}

	r.endTask(a)

	select {
	case got := <-claimed:
		if !got {
			t.Fatal("blocked task not admitted once a slot freed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second task never admitted after the slot freed")
	}
}

func TestTaskSlotIsPerTaskNotPerRun(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	r.taskSlots.setLimit(1)

	taskID := uuid.New()
	first, second := jobFor(taskID), jobFor(taskID)
	if !r.beginTask(context.Background(), first) {
		t.Fatal("first run must start")
	}
	if r.beginTask(context.Background(), second) {
		t.Fatal("second run must park on the same task instead of paying for another slot")
	}
	r.endTask(taskID)
	released := <-r.queue
	if !r.beginTask(context.Background(), released) {
		t.Fatal("released run must claim the task without a second slot")
	}
}

func TestSlotGateAdmitsUpToLimit(t *testing.T) {
	g := newSlotGate()
	g.setLimit(2)
	for i := 0; i < 2; i++ {
		if !g.acquire(context.Background()) {
			t.Fatal("an admit under the limit refused")
		}
	}
	g.release()
	if !g.acquire(context.Background()) {
		t.Fatal("a slot freed by release not admitted")
	}
}

func TestSlotGateBlocksBeyondLimitUntilRelease(t *testing.T) {
	g := newSlotGate()
	g.setLimit(1)
	if !g.acquire(context.Background()) {
		t.Fatal("first admit refused")
	}
	waited := make(chan struct{})
	go func() {
		g.acquire(context.Background())
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("acquire returned past the limit")
	default:
	}
	g.release()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("waiting acquire never admitted on release")
	}
}

func TestSlotGateUnlimitedAtZeroLimit(t *testing.T) {
	g := newSlotGate()
	for i := 0; i < 32; i++ {
		if !g.acquire(context.Background()) {
			t.Fatal("unlimited acquire refused")
		}
	}
}

func TestSlotGateCancelledAcquire(t *testing.T) {
	g := newSlotGate()
	g.setLimit(1)
	if !g.acquire(context.Background()) {
		t.Fatal("first admit refused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()
	if g.acquire(ctx) {
		t.Fatal("acquire succeeded on a cancelled context")
	}
}

func TestSlotGateRaisingLimitWakesWaiters(t *testing.T) {
	g := newSlotGate()
	g.setLimit(1)
	if !g.acquire(context.Background()) {
		t.Fatal("first admit refused")
	}
	waited := make(chan bool, 1)
	go func() { waited <- g.acquire(context.Background()) }()
	time.Sleep(75 * time.Millisecond)
	g.setLimit(2)
	select {
	case got := <-waited:
		if !got {
			t.Fatal("woken waiter not admitted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter never admitted after the limit rose")
	}
}
