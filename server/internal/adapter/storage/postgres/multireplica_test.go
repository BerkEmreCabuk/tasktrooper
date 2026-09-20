package postgres_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Two replicas, one real database.
//
// Everything else in this repository tests the SQL through a Go fake that
// promises to behave the way the statement does. This file does not: it starts
// the embedded Postgres the migrations already run against, opens TWO POOLS
// over it — which is what two pods are — and races them.
//
// That distinction is the whole reason the file exists. The claim, the guarded
// stale write and the delivery ledger are single statements whose correctness
// IS their concurrency behaviour; a fake that serialises them under a mutex
// proves that the caller asks the right question, not that the answer is right.
// FOR UPDATE SKIP LOCKED, ON CONFLICT DO NOTHING and a WHERE clause that
// re-reads what the caller was told are only worth anything if Postgres is the
// one running them.

type replicaFixture struct {
	pg       *database.Embedded
	poolA    *pgxpool.Pool
	poolB    *pgxpool.Pool
	a        *pgstore.DB
	b        *pgstore.DB
	repoID   uuid.UUID
	agentID  uuid.UUID
	agentID2 uuid.UUID
	eventID  uuid.UUID
	nextTask int
}

func newReplicaFixture(t *testing.T) *replicaFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pg, err := newTestDatabase(ctx)
	if err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Stop() })

	dsn := pg.DSN()

	// Two pools, not two handles on one pool. A pod is a pool: separate
	// connections, separate transactions, no shared Go state whatsoever — which
	// is exactly the thing every in-memory guard this work replaced assumed it
	// had.
	poolA, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool A: %v", err)
	}
	t.Cleanup(poolA.Close)
	poolB, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool B: %v", err)
	}
	t.Cleanup(poolB.Close)

	f := &replicaFixture{
		pg:       pg,
		poolA:    poolA,
		poolB:    poolB,
		a:        pgstore.NewDB(poolA),
		b:        pgstore.NewDB(poolB),
		nextTask: 1,
	}
	f.seed(t)
	return f
}

// seed writes the minimum a run needs to exist: a repository, an agent and the
// board event a run points at. Raw SQL rather than the stores, because what is
// under test is one statement and everything else is scaffold.
func (f *replicaFixture) seed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := f.a.QueryRow(ctx, `
		INSERT INTO repositories (name, root_path) VALUES ('probe', '/tmp/probe') RETURNING id
	`).Scan(&f.repoID); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	if err := f.a.QueryRow(ctx, `
		INSERT INTO agents (name) VALUES ('developer') RETURNING id
	`).Scan(&f.agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	// A second agent, because idx_task_agent_runs_one_pending is unique over
	// (task_id, agent_id) WHERE status='pending' — two queued runs for one task
	// are legal only when different agents own them, which is exactly the case
	// the one-run-per-task claim exists to arbitrate.
	if err := f.a.QueryRow(ctx, `
		INSERT INTO agents (name) VALUES ('architect') RETURNING id
	`).Scan(&f.agentID2); err != nil {
		t.Fatalf("seed second agent: %v", err)
	}
	// One event is enough for every run in these tests: board_event_id is
	// provenance, and nothing the claim does reads it.
	taskID := f.newTask(t)
	if err := f.a.QueryRow(ctx, `
		INSERT INTO board_events (repository_id, task_id, event_type)
		VALUES ($1, $2, 'task.moved') RETURNING id
	`, f.repoID, taskID).Scan(&f.eventID); err != nil {
		t.Fatalf("seed board event: %v", err)
	}
}

func (f *replicaFixture) newTask(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	number := f.nextTask
	f.nextTask++
	if err := f.a.QueryRow(context.Background(), `
		INSERT INTO board_tasks (repository_id, title, task_number)
		VALUES ($1, 'probe task', $2) RETURNING id
	`, f.repoID, number).Scan(&id); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return id
}

func (f *replicaFixture) newPendingRun(t *testing.T, taskID uuid.UUID) uuid.UUID {
	return f.newPendingRunFor(t, taskID, f.agentID)
}

func (f *replicaFixture) newPendingRunFor(t *testing.T, taskID, agentID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.a.QueryRow(context.Background(), `
		INSERT INTO task_agent_runs (task_id, agent_id, board_event_id, status)
		VALUES ($1, $2, $3, 'pending') RETURNING id
	`, taskID, agentID, f.eventID).Scan(&id); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return id
}

func (f *replicaFixture) status(t *testing.T, runID uuid.UUID) string {
	t.Helper()
	var status string
	if err := f.a.QueryRow(context.Background(), `SELECT status FROM task_agent_runs WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

// raceClaims fires one ClaimRun per (store, run) pair at the same instant and
// returns how many were granted.
func raceClaims(t *testing.T, ctx context.Context, pairs []claimPair) int32 {
	t.Helper()
	var granted atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, p := range pairs {
		wg.Add(1)
		go func(p claimPair) {
			defer wg.Done()
			<-start
			res, err := pgstore.NewTaskAgentRunStore(p.db).ClaimRun(ctx, p.claim)
			if err != nil {
				t.Errorf("ClaimRun: %v", err)
				return
			}
			if res.Claimed {
				granted.Add(1)
			}
		}(p)
	}
	close(start)
	wg.Wait()
	return granted.Load()
}

type claimPair struct {
	db    *pgstore.DB
	claim port.RunClaim
}

// One run per task, decided by Postgres.
//
// Runner.beginTask held this in a Go map: two replicas each believed they held
// the task and both started an agent on the same branch and the same checkout.
// Two pools, two pending runs on one task, one instant — one winner.
func TestClaimRunGivesOneReplicaTheTask(t *testing.T) {
	f := newReplicaFixture(t)
	ctx := context.Background()

	taskID := f.newTask(t)
	runA := f.newPendingRunFor(t, taskID, f.agentID)
	runB := f.newPendingRunFor(t, taskID, f.agentID2)

	granted := raceClaims(t, ctx, []claimPair{
		{f.a, port.RunClaim{RunID: runA, TaskID: taskID, LiveWithin: time.Minute}},
		{f.b, port.RunClaim{RunID: runB, TaskID: taskID, LiveWithin: time.Minute}},
	})
	if granted != 1 {
		t.Fatalf("%d replicas claimed the same task, want 1", granted)
	}

	// And the loser's row is untouched, which is what makes the refusal a
	// durable park rather than a lost job: the reconciler re-dispatches it.
	running, pending := 0, 0
	for _, id := range []uuid.UUID{runA, runB} {
		switch f.status(t, id) {
		case domain.TaskAgentRunStatusRunning:
			running++
		case domain.TaskAgentRunStatusPending:
			pending++
		}
	}
	if running != 1 || pending != 1 {
		t.Fatalf("running=%d pending=%d, want 1 and 1", running, pending)
	}
}

// The same run, claimed from both replicas at once. FOR UPDATE SKIP LOCKED is
// what makes the loser see zero rows instead of waiting and then overwriting.
func TestClaimRunOnTheSameRowHasOneWinner(t *testing.T) {
	f := newReplicaFixture(t)
	ctx := context.Background()

	taskID := f.newTask(t)
	runID := f.newPendingRun(t, taskID)

	granted := raceClaims(t, ctx, []claimPair{
		{f.a, port.RunClaim{RunID: runID, TaskID: taskID, LiveWithin: time.Minute}},
		{f.b, port.RunClaim{RunID: runID, TaskID: taskID, LiveWithin: time.Minute}},
	})
	if granted != 1 {
		t.Fatalf("%d replicas claimed one run, want 1", granted)
	}
}

// A run whose owner died does not hold the budget forever. The claim counts
// only rows whose heartbeat is inside LiveWithin, so a 'running' row nobody is
// touching stops blocking the task within one staleness window.
func TestClaimRunIgnoresRunsWithNoHeartbeat(t *testing.T) {
	f := newReplicaFixture(t)
	ctx := context.Background()

	taskID := f.newTask(t)
	abandoned := f.newPendingRun(t, taskID)
	if _, err := f.a.Exec(ctx, `
		UPDATE task_agent_runs SET status = 'running', updated_at = now() - interval '1 hour' WHERE id = $1
	`, abandoned); err != nil {
		t.Fatalf("age the abandoned run: %v", err)
	}

	fresh := f.newPendingRunFor(t, taskID, f.agentID2)
	res, err := pgstore.NewTaskAgentRunStore(f.b).ClaimRun(ctx, port.RunClaim{
		RunID: fresh, TaskID: taskID, LiveWithin: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if !res.Claimed {
		t.Fatalf("a task held only by a dead run must be claimable, got reason %q", res.Reason)
	}
}

// The reconciler lists stale runs on one replica and writes on another instant.
// FailIfStale re-asserts the cutoff inside the write, so an owner that
// heartbeats in between keeps its run — which is the whole difference between
// a sweep and a hazard once there is more than one pod sweeping.
func TestFailIfStaleLosesToAHeartbeat(t *testing.T) {
	f := newReplicaFixture(t)
	ctx := context.Background()

	taskID := f.newTask(t)
	runID := f.newPendingRun(t, taskID)
	if _, err := f.a.Exec(ctx, `
		UPDATE task_agent_runs SET status = 'running', updated_at = now() - interval '1 hour' WHERE id = $1
	`, runID); err != nil {
		t.Fatalf("age the run: %v", err)
	}

	// What the sweep decided when it read the list.
	cutoff := time.Now().Add(-30 * time.Minute)

	// What the owner does before the sweep gets to the write, from the other
	// replica.
	if _, err := pgstore.NewTaskAgentRunStore(f.b).Touch(ctx, runID); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	failed, err := pgstore.NewTaskAgentRunStore(f.a).FailIfStale(ctx, runID, cutoff, "recovered")
	if err != nil {
		t.Fatalf("FailIfStale: %v", err)
	}
	if failed {
		t.Fatal("a run whose owner heartbeated mid-sweep must not be marked failed")
	}
	if got := f.status(t, runID); got != domain.TaskAgentRunStatusRunning {
		t.Fatalf("status = %q, want running", got)
	}
}

// Touch reads the status back on the write it was making anyway, which is how a
// stop crosses processes: the request flips the row from whichever replica
// served it, and the replica actually executing learns about it on its next
// beat.
func TestTouchReportsACancelWrittenByAnotherReplica(t *testing.T) {
	f := newReplicaFixture(t)
	ctx := context.Background()

	taskID := f.newTask(t)
	runID := f.newPendingRun(t, taskID)
	if _, err := pgstore.NewTaskAgentRunStore(f.a).ClaimRun(ctx, port.RunClaim{
		RunID: runID, TaskID: taskID, LiveWithin: time.Minute,
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// The stop request, served by the replica that is NOT executing.
	if _, ok, err := pgstore.NewTaskAgentRunStore(f.b).CancelIfLive(ctx, runID, "user stopped"); err != nil || !ok {
		t.Fatalf("CancelIfLive on the other replica: ok=%v err=%v", ok, err)
	}

	status, err := pgstore.NewTaskAgentRunStore(f.a).Touch(ctx, runID)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if status != domain.TaskAgentRunStatusCancelled {
		t.Fatalf("the executing replica read %q from its heartbeat, want cancelled", status)
	}
}

// GitHub retries a delivery to whichever pod the load balancer picks. The
// insert is the check, so exactly one replica may treat a delivery as new
// however many arrive together.
func TestWebhookDeliveryDedupeAcrossReplicas(t *testing.T) {
	f := newReplicaFixture(t)
	ctx := context.Background()

	const delivery = "9f1c0f8e-0000-4000-8000-000000000001"
	var first atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, db := range []*pgstore.DB{f.a, f.b, f.a, f.b} {
		wg.Add(1)
		go func(db *pgstore.DB) {
			defer wg.Done()
			<-start
			fresh, err := pgstore.NewWebhookDeliveryStore(db).MarkSeen(ctx, delivery, time.Hour)
			if err != nil {
				t.Errorf("MarkSeen: %v", err)
				return
			}
			if fresh {
				first.Add(1)
			}
		}(db)
	}
	close(start)
	wg.Wait()

	if got := first.Load(); got != 1 {
		t.Fatalf("%d replicas treated one delivery as new, want 1", got)
	}
}
