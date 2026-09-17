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
)

// The released column is split between two queries and the split is pure SQL:
// which side a card falls on is decided by a lateral join onto its open column
// span, and the archive's search runs in the database. Neither can be checked
// without a real Postgres — a Go-level fake would answer whatever it was told.
type BoardTaskReleasedSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	tasks  *postgres.BoardTaskStore
	spans  *postgres.TaskColumnSpanStore
	repoID uuid.UUID
}

func TestBoardTaskReleasedSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(BoardTaskReleasedSuite))
}

func (s *BoardTaskReleasedSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: sharedPGRuntimeDir,
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.spans = postgres.NewTaskColumnSpanStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "board-task-released-test", "", "/tmp/board-task-released-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *BoardTaskReleasedSuite) TearDownSuite() {
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

// newTask creates a card and, when enteredAt is non-zero, records the span that
// says when it arrived in its column — which is what the window reads.
func (s *BoardTaskReleasedSuite) newTask(title string, column domain.TaskColumn, enteredAt time.Time) domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        title,
		Description:  title + " description",
		TaskType:     domain.TaskTypeTask,
		Column:       column,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	if !enteredAt.IsZero() {
		s.Require().NoError(s.spans.RecordMove(s.ctx, s.repoID, task.ID, string(column), enteredAt))
	}
	return task
}

func (s *BoardTaskReleasedSuite) TestBoardDropsReleasedTasksPastTheWindow() {
	now := time.Now()
	fresh := s.newTask("released yesterday", domain.TaskColumnReleased, now.Add(-24*time.Hour))
	stale := s.newTask("released last month", domain.TaskColumnReleased, now.Add(-30*24*time.Hour))
	active := s.newTask("still in progress", domain.TaskColumnInProgress, now.Add(-30*24*time.Hour))

	visible, err := s.tasks.ListBoardVisible(s.ctx, now.Add(-domain.ReleasedBoardWindow))
	s.Require().NoError(err)

	ids := map[uuid.UUID]bool{}
	for _, task := range visible {
		ids[task.ID] = true
	}
	s.True(ids[fresh.ID], "a task released inside the window belongs on the board")
	s.True(ids[active.ID], "the window applies to released only — an old task in another column stays")
	s.False(ids[stale.ID], "a task released before the cutoff is off the board")

	// Off the board is not gone: ListAll is what the sweepers and id lookups
	// read, and it must still see every task.
	all, err := s.tasks.ListAll(s.ctx)
	s.Require().NoError(err)
	found := false
	for _, task := range all {
		if task.ID == stale.ID {
			found = true
		}
	}
	s.True(found, "the archived task must still exist for ListAll")
}

func (s *BoardTaskReleasedSuite) TestArchiveListsAndSearches() {
	now := time.Now()
	older := s.newTask("csv export shipped", domain.TaskColumnReleased, now.Add(-40*24*time.Hour))
	newer := s.newTask("payment retry shipped", domain.TaskColumnReleased, now.Add(-2*24*time.Hour))

	all, err := s.tasks.ListReleasedArchive(s.ctx, "", 100)
	s.Require().NoError(err)
	s.Require().NotEmpty(all)

	// Newest release first: the archive is read from the top.
	var olderPos, newerPos = -1, -1
	for i, task := range all {
		switch task.ID {
		case older.ID:
			olderPos = i
		case newer.ID:
			newerPos = i
		}
		s.Equal(domain.TaskColumnReleased, task.Column, "the archive holds released tasks only")
	}
	s.Require().NotEqual(-1, olderPos)
	s.Require().NotEqual(-1, newerPos)
	s.Less(newerPos, olderPos, "the most recently released task comes first")

	byTitle, err := s.tasks.ListReleasedArchive(s.ctx, "csv export", 100)
	s.Require().NoError(err)
	s.Require().Len(byTitle, 1)
	s.Equal(older.ID, byTitle[0].ID)

	byKey, err := s.tasks.ListReleasedArchive(s.ctx, domain.FormatTaskKey(newer.TaskType, newer.TaskNumber), 100)
	s.Require().NoError(err)
	s.Require().Len(byKey, 1)
	s.Equal(newer.ID, byKey[0].ID)

	none, err := s.tasks.ListReleasedArchive(s.ctx, "nothing matches this", 100)
	s.Require().NoError(err)
	s.Empty(none)
}
