package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// TaskAgentRunStoreSuite covers migration 075's partial unique index —
// idx_task_agent_runs_one_pending ON task_agent_runs (task_id, agent_id)
// WHERE status = 'pending' — and the ON CONFLICT clause Create relies on to
// enforce it. That SQL was only ever verified by hand against a real
// Postgres; nothing in CI pins it, so a future edit to either the index or
// the ON CONFLICT target could silently break the dispatcher's
// at-most-one-pending-run guarantee. This suite runs against a real
// (embedded) Postgres rather than a fake, because the guarantee lives
// entirely in the database, not in Go.
type TaskAgentRunStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.TaskAgentRunStore
	tasks  *postgres.BoardTaskStore
	agents *postgres.CatalogStore
	events *postgres.BoardEventStore
	repoID uuid.UUID
}

func TestTaskAgentRunStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(TaskAgentRunStoreSuite))
}

func (s *TaskAgentRunStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	// Its own runtime path, for the same reason IncidentStoreSuite has one: a
	// shared binaries cache makes concurrent initdb runs fight.
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	// Stores take the tenant-scoped handle now, and every statement it issues
	// reads app.tenant_id off the context - so the suite has to BE a tenant.
	// A fresh uuid per suite means two suites sharing an embedded Postgres
	// cannot see each other's rows, which is the property under test anyway.
	s.db = postgres.NewDB(pool)
	s.ctx = tenant.With(s.ctx, tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner})
	s.store = postgres.NewTaskAgentRunStore(s.db)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.agents = postgres.NewCatalogStore(s.db)
	s.events = postgres.NewBoardEventStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "task-agent-run-test", "", "/tmp/task-agent-run-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *TaskAgentRunStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// newTask creates a fresh board task. Each test gets its own because the
// guarantee under test is keyed on task_id.
func (s *TaskAgentRunStoreSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "task-agent-run-test",
		TaskType:     domain.TaskTypeTask,
		Column:       domain.TaskColumn("backlog"),
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

// newAgent creates a fresh agent. agents.name is unique, so each test needs
// its own rather than sharing one across the suite.
func (s *TaskAgentRunStoreSuite) newAgent() domain.Agent {
	agent, err := s.agents.CreateAgent(s.ctx, domain.Agent{
		Name:         "task-agent-run-test-" + uuid.New().String(),
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "test-model",
	})
	s.Require().NoError(err)
	return agent
}

// newEvent creates the board event a run is attributed to. board_event_id is
// a required, non-null FK on task_agent_runs, and dispatch mints a fresh
// event per attempt — including per racer in the duplicate-dispatch scenario
// migration 075 guards against — so tests that simulate a race pass a new
// event for each Create call even though task_id/agent_id repeat.
func (s *TaskAgentRunStoreSuite) newEvent(taskID uuid.UUID) domain.BoardEvent {
	event, err := s.events.Create(s.ctx, domain.BoardEvent{
		RepositoryID: s.repoID,
		TaskID:       taskID,
		EventType:    domain.BoardEventTaskAssigned,
		Payload:      json.RawMessage(`{}`),
	})
	s.Require().NoError(err)
	return event
}

// TestCreateFirstPendingRunSucceeds is the baseline: nothing queued yet, so
// Create must insert and return a live pending row.
func (s *TaskAgentRunStoreSuite) TestCreateFirstPendingRunSucceeds() {
	task := s.newTask()
	agent := s.newAgent()
	event := s.newEvent(task.ID)

	run, err := s.store.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       task.ID,
		AgentID:      agent.ID,
		BoardEventID: event.ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, run.ID)
	s.Equal(domain.TaskAgentRunStatusPending, run.Status)
	s.Equal(task.ID, run.TaskID)
	s.Equal(agent.ID, run.AgentID)
	s.Equal(event.ID, run.BoardEventID)
}

// TestSecondPendingCreateForSameTaskAgentReturnsTheWinnersRowNotAnError pins
// the documented ON CONFLICT behaviour in TaskAgentRunStore.Create: migration
// 075's partial unique index makes the loser's insert conflict, and
// `DO UPDATE SET updated_at = task_agent_runs.updated_at` (a deliberate
// no-op) turns that conflict into a lookup that hands back the row that won
// the race — same ID, same original board_event_id — rather than surfacing
// an error or creating a second row.
func (s *TaskAgentRunStoreSuite) TestSecondPendingCreateForSameTaskAgentReturnsTheWinnersRowNotAnError() {
	task := s.newTask()
	agent := s.newAgent()
	firstEvent := s.newEvent(task.ID)
	secondEvent := s.newEvent(task.ID)

	first, err := s.store.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       task.ID,
		AgentID:      agent.ID,
		BoardEventID: firstEvent.ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err)

	second, err := s.store.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       task.ID,
		AgentID:      agent.ID,
		BoardEventID: secondEvent.ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err, "a duplicate pending dispatch must degrade to a no-op lookup, not an error")
	s.Equal(first.ID, second.ID, "the loser must get back the winner's row, not create its own")
	s.Equal(firstEvent.ID, second.BoardEventID, "the winner's row must not be rewritten with the loser's board_event_id")

	runs, err := s.store.ListByTask(s.ctx, task.ID, 50)
	s.Require().NoError(err)
	s.Len(runs, 1, "the partial unique index must prevent a second row from ever existing")
}

// TestPendingSlotReopensOnceTheFirstRunLeavesPending is the index's other
// half: the predicate is 'pending' only, so once a run moves to any terminal
// status (or back to running-elsewhere), the slot for that (task, agent) is
// free again and a fresh dispatch must be able to queue.
func (s *TaskAgentRunStoreSuite) TestPendingSlotReopensOnceTheFirstRunLeavesPending() {
	task := s.newTask()
	agent := s.newAgent()

	first, err := s.store.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       task.ID,
		AgentID:      agent.ID,
		BoardEventID: s.newEvent(task.ID).ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err)

	first.Status = domain.TaskAgentRunStatusCompleted
	_, err = s.store.Update(s.ctx, first)
	s.Require().NoError(err)

	second, err := s.store.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       task.ID,
		AgentID:      agent.ID,
		BoardEventID: s.newEvent(task.ID).ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err)
	s.NotEqual(first.ID, second.ID, "a completed run must not block a new pending run for the same task+agent")
	s.Equal(domain.TaskAgentRunStatusPending, second.Status)

	runs, err := s.store.ListByTask(s.ctx, task.ID, 50)
	s.Require().NoError(err)
	s.Len(runs, 2, "both the completed run and the new pending run must exist")
}

// TestTwoDifferentTasksEachHoldAPendingRunSimultaneously proves the index is
// scoped per (task_id, agent_id) rather than globally: the same agent can
// have a pending run queued on two different tasks at once.
func (s *TaskAgentRunStoreSuite) TestTwoDifferentTasksEachHoldAPendingRunSimultaneously() {
	agent := s.newAgent()
	taskA := s.newTask()
	taskB := s.newTask()

	runA, err := s.store.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       taskA.ID,
		AgentID:      agent.ID,
		BoardEventID: s.newEvent(taskA.ID).ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err)

	runB, err := s.store.Create(s.ctx, domain.TaskAgentRun{
		TaskID:       taskB.ID,
		AgentID:      agent.ID,
		BoardEventID: s.newEvent(taskB.ID).ID,
		Status:       domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err, "a pending run on one task must never block a pending run on a different task")
	s.NotEqual(runA.ID, runB.ID)
	s.Equal(domain.TaskAgentRunStatusPending, runA.Status)
	s.Equal(domain.TaskAgentRunStatusPending, runB.Status)
}

// The task key is what a branch, a PR title and every chat refer to a task by,
// so a number must never be handed out twice — including after the task that
// held it is deleted, which the old MAX(task_number)+1 could not promise. The
// counters are per type, so the three sequences do not interleave.
func (s *TaskAgentRunStoreSuite) TestTaskNumbersAreMonotonicPerType() {
	first, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeBug)
	s.Require().NoError(err)
	second, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeBug)
	s.Require().NoError(err)
	s.Equal(first+1, second)

	// A different type counts on its own, from its own floor.
	analiz, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeAnaliz)
	s.Require().NoError(err)
	analizAgain, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeAnaliz)
	s.Require().NoError(err)
	s.Equal(analiz+1, analizAgain)

	// The number of a deleted task is gone for good: a task created after the
	// delete gets a fresh number, not the one just freed.
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   second,
		Title:        "counter fixture",
		TaskType:     domain.TaskTypeBug,
		Column:       domain.TaskColumnBacklog,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	s.Equal(fmt.Sprintf("B-%d", second), task.Key)
	s.Require().NoError(s.tasks.Delete(s.ctx, s.repoID, task.ID))

	afterDelete, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeBug)
	s.Require().NoError(err)
	s.Greater(afterDelete, second)
}
