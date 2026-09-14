package postgres_test

import (
	"context"
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

// BoardTaskAssigneeSuite covers the column that decides WHOSE MAC a task runs
// on (assignee_user_id, migration 115) surviving a write-read cycle.
//
// Update wrote every other field of the task and simply omitted this one, so a
// re-assignment through the API returned the new person, logged the new person,
// and left the row pointing at the old one — invisible until a run went to the
// wrong laptop. Nothing in Go can catch that: the store hands back its own
// RETURNING row, so only a real statement against a real column can say whether
// the value was written.
type BoardTaskAssigneeSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	tasks  *postgres.BoardTaskStore
	repoID uuid.UUID
}

func TestBoardTaskAssigneeSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(BoardTaskAssigneeSuite))
}

func (s *BoardTaskAssigneeSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
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
	db := postgres.NewDB(pool)
	s.ctx = tenant.With(s.ctx, tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner})
	s.tasks = postgres.NewBoardTaskStore(db)

	repo, err := postgres.NewRepositoryStore(db).Create(s.ctx, "board-task-assignee-test", "",
		"/tmp/board-task-assignee-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *BoardTaskAssigneeSuite) TearDownSuite() {
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

func (s *BoardTaskAssigneeSuite) createTask(assignee string) domain.BoardTask {
	number, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID:   s.repoID,
		TaskNumber:     number,
		Title:          "assignee round trip",
		TaskType:       domain.TaskTypeTask,
		Column:         domain.TaskColumnBacklog,
		Priority:       domain.TaskPriorityMedium,
		CreatedBy:      "user",
		AssigneeUserID: assignee,
	})
	s.Require().NoError(err)
	return task
}

func (s *BoardTaskAssigneeSuite) reread(id uuid.UUID) domain.BoardTask {
	task, err := s.tasks.Get(s.ctx, s.repoID, id)
	s.Require().NoError(err)
	return task
}

func (s *BoardTaskAssigneeSuite) TestCreateAndReadBackTheAssignee() {
	task := s.createTask("uid-ayse")
	s.Equal("uid-ayse", task.AssigneeUserID)
	s.Equal("uid-ayse", s.reread(task.ID).AssigneeUserID)
}

func (s *BoardTaskAssigneeSuite) TestUpdateWritesANewAssignee() {
	task := s.createTask("uid-ayse")

	task.AssigneeUserID = "uid-mehmet"
	updated, err := s.tasks.Update(s.ctx, task)
	s.Require().NoError(err)

	s.Equal("uid-mehmet", updated.AssigneeUserID, "the RETURNING row carries the new assignee")
	s.Equal("uid-mehmet", s.reread(task.ID).AssigneeUserID, "and so does a fresh read")
}

func (s *BoardTaskAssigneeSuite) TestUpdateClearsTheAssignee() {
	task := s.createTask("uid-ayse")

	task.AssigneeUserID = ""
	_, err := s.tasks.Update(s.ctx, task)
	s.Require().NoError(err)

	s.Empty(s.reread(task.ID).AssigneeUserID)
	var null bool
	s.Require().NoError(s.pool.QueryRow(context.Background(),
		`SELECT assignee_user_id IS NULL FROM board_tasks WHERE id = $1`, task.ID).Scan(&null))
	s.True(null, "unassigned is NULL, so the partial index stays the size of the assigned cards")
}

// Every other field goes through Update untouched on a request that only moves
// a card, and the assignee has to behave the same way: UpdateTask always hands
// this store a task it just read.
func (s *BoardTaskAssigneeSuite) TestUpdateRoundTripsAnUntouchedAssignee() {
	task := s.createTask("uid-ayse")

	task.Column = domain.TaskColumnTodo
	updated, err := s.tasks.Update(s.ctx, task)
	s.Require().NoError(err)

	s.Equal(domain.TaskColumnTodo, updated.Column)
	s.Equal("uid-ayse", s.reread(task.ID).AssigneeUserID)
}
