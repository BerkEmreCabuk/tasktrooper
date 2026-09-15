package postgres_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// ActorUserIDSuite covers board_events.actor_user_id and
// task_comments.actor_user_id: written when a human's request carried a
// verified X-Internal-Actor uid, left NULL otherwise, and round-tripped
// unchanged through every list path.
type ActorUserIDSuite struct {
	suite.Suite
	ctx      context.Context
	cancel   context.CancelFunc
	pg       *database.Embedded
	pool     *pgxpool.Pool
	db       *postgres.DB
	tasks    *postgres.BoardTaskStore
	events   *postgres.BoardEventStore
	comments *postgres.TaskCommentStore
	repoID   uuid.UUID
}

func TestActorUserIDSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ActorUserIDSuite))
}

func (s *ActorUserIDSuite) SetupSuite() {
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
	s.db = postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.events = postgres.NewBoardEventStore(s.db)
	s.comments = postgres.NewTaskCommentStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "actor-user-id-test", "", "/tmp/actor-user-id-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *ActorUserIDSuite) TearDownSuite() {
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

func (s *ActorUserIDSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "actor-user-id-test",
		TaskType:     domain.TaskTypeTask,
		Column:       domain.TaskColumnTodo,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

func (s *ActorUserIDSuite) TestBoardEventActorUserIDRoundTrips() {
	task := s.newTask()
	uid := "firebase-uid-" + uuid.New().String()

	created, err := s.events.Create(s.ctx, domain.BoardEvent{
		RepositoryID: s.repoID,
		TaskID:       task.ID,
		EventType:    domain.BoardEventTaskMoved,
		Payload:      json.RawMessage(`{}`),
		ActorUserID:  &uid,
	})
	s.Require().NoError(err)
	s.Require().NotNil(created.ActorUserID)
	s.Equal(uid, *created.ActorUserID)

	listed, err := s.events.ListByTask(s.ctx, task.ID, 10)
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Require().NotNil(listed[0].ActorUserID)
	s.Equal(uid, *listed[0].ActorUserID)
}

func (s *ActorUserIDSuite) TestBoardEventActorUserIDNilWhenNotProvided() {
	task := s.newTask()

	created, err := s.events.Create(s.ctx, domain.BoardEvent{
		RepositoryID: s.repoID,
		TaskID:       task.ID,
		EventType:    domain.BoardEventTaskMoved,
		Payload:      json.RawMessage(`{}`),
	})
	s.Require().NoError(err)
	s.Nil(created.ActorUserID)

	listed, err := s.events.ListByTask(s.ctx, task.ID, 10)
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Nil(listed[0].ActorUserID)
}

func (s *ActorUserIDSuite) TestTaskCommentActorUserIDRoundTrips() {
	task := s.newTask()
	uid := "firebase-uid-" + uuid.New().String()

	created, err := s.comments.Create(s.ctx, domain.TaskComment{
		TaskID:      task.ID,
		AuthorType:  "user",
		Content:     "looks good",
		ActorUserID: &uid,
	})
	s.Require().NoError(err)
	s.Require().NotNil(created.ActorUserID)
	s.Equal(uid, *created.ActorUserID)

	listed, err := s.comments.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Require().NotNil(listed[0].ActorUserID)
	s.Equal(uid, *listed[0].ActorUserID)
}

func (s *ActorUserIDSuite) TestTaskCommentActorUserIDNilWhenNotProvided() {
	task := s.newTask()

	created, err := s.comments.Create(s.ctx, domain.TaskComment{
		TaskID:     task.ID,
		AuthorType: "agent",
		Content:    "automated update",
	})
	s.Require().NoError(err)
	s.Nil(created.ActorUserID)

	listed, err := s.comments.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Nil(listed[0].ActorUserID)
}
