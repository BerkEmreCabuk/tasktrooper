package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// AcceptanceCriterionSuite covers ReplaceForTask's diff-and-upsert behaviour:
// resending an unchanged criterion must not churn its id or reset state the
// agent already recorded on it.
type AcceptanceCriterionSuite struct {
	suite.Suite
	ctx      context.Context
	cancel   context.CancelFunc
	pg       *database.Embedded
	pool     *pgxpool.Pool
	db       *postgres.DB
	tasks    *postgres.BoardTaskStore
	criteria *postgres.AcceptanceCriterionStore
	repoID   uuid.UUID
}

func TestAcceptanceCriterionSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(AcceptanceCriterionSuite))
}

func (s *AcceptanceCriterionSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.criteria = postgres.NewAcceptanceCriterionStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "acceptance-criterion-test", "", "/tmp/acceptance-criterion-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *AcceptanceCriterionSuite) TearDownSuite() {
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

func (s *AcceptanceCriterionSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "acceptance-criterion-test",
		TaskType:     "task",
		Column:       domain.TaskColumnInProgress,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

// A resend of the same texts plus one new item must keep the original ids and
// the already-ticked criterion's completed state, while the new item gets a
// fresh id — the id/state churn this fix removes.
func (s *AcceptanceCriterionSuite) TestReplaceForTaskKeepsIdsAndStateForUnchangedText() {
	task := s.newTask()
	initial := []domain.AcceptanceCriterionInput{
		{Text: "The endpoint returns 201", Position: 1},
		{Text: "The response carries the task id", Position: 2},
		{Text: "The event is journaled", Position: 3},
	}
	created, err := s.criteria.ReplaceForTask(s.ctx, task.ID, initial)
	s.Require().NoError(err)
	s.Require().Len(created, 3)

	ticked, err := s.criteria.UpdateCompleted(s.ctx, created[0].ID, true)
	s.Require().NoError(err)
	s.True(ticked.Completed)

	resend := []domain.AcceptanceCriterionInput{
		{Text: "The endpoint returns 201", Position: 1},
		{Text: "The response carries the task id", Position: 2},
		{Text: "The event is journaled", Position: 3},
		{Text: "A new criterion added later", Position: 4},
	}
	replaced, err := s.criteria.ReplaceForTask(s.ctx, task.ID, resend)
	s.Require().NoError(err)
	s.Require().Len(replaced, 4)

	byText := make(map[string]domain.AcceptanceCriterion, len(replaced))
	for _, c := range replaced {
		byText[c.Text] = c
	}

	s.Equal(created[0].ID, byText["The endpoint returns 201"].ID, "unchanged text keeps its id")
	s.True(byText["The endpoint returns 201"].Completed, "unchanged text keeps its completed state")
	s.Equal(created[1].ID, byText["The response carries the task id"].ID)
	s.Equal(created[2].ID, byText["The event is journaled"].ID)

	newItem, ok := byText["A new criterion added later"]
	s.Require().True(ok)
	s.NotEqual(uuid.Nil, newItem.ID)
	for _, c := range created {
		s.NotEqual(c.ID, newItem.ID, "the new item must not reuse an existing id")
	}
}

// A criterion omitted from a resend is still deleted — that half of the
// documented "send the full list" contract does not change.
func (s *AcceptanceCriterionSuite) TestReplaceForTaskDeletesOmittedCriteria() {
	task := s.newTask()
	initial := []domain.AcceptanceCriterionInput{
		{Text: "Kept criterion", Position: 1},
		{Text: "Dropped criterion", Position: 2},
	}
	_, err := s.criteria.ReplaceForTask(s.ctx, task.ID, initial)
	s.Require().NoError(err)

	replaced, err := s.criteria.ReplaceForTask(s.ctx, task.ID, []domain.AcceptanceCriterionInput{
		{Text: "Kept criterion", Position: 1},
	})
	s.Require().NoError(err)
	s.Require().Len(replaced, 1)
	s.Equal("Kept criterion", replaced[0].Text)

	listed, err := s.criteria.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
}

// A reviewer's verdict on a criterion whose text did not change must survive
// a full replace — the ON DELETE CASCADE from task_criterion_checks only fires
// today because the row itself gets deleted and reinserted for no reason.
func (s *AcceptanceCriterionSuite) TestReplaceForTaskPreservesReviewVerdictForUnchangedText() {
	task := s.newTask()
	created, err := s.criteria.ReplaceForTask(s.ctx, task.ID, []domain.AcceptanceCriterionInput{
		{Text: "The build is green", Position: 1},
	})
	s.Require().NoError(err)
	s.Require().Len(created, 1)

	check, err := s.criteria.UpsertCheck(s.ctx, domain.CriterionCheck{
		CriterionID: created[0].ID,
		Role:        domain.CriterionReviewRoleQA,
		Approved:    true,
		Note:        "verified locally",
	})
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, check.ID)

	_, err = s.criteria.ReplaceForTask(s.ctx, task.ID, []domain.AcceptanceCriterionInput{
		{Text: "The build is green", Position: 1},
	})
	s.Require().NoError(err)

	listed, err := s.criteria.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Require().Len(listed[0].Checks, 1, "the review verdict must survive a same-text replace")
	s.True(listed[0].Checks[0].Approved)
}

func (s *AcceptanceCriterionSuite) TestGetCriterionReturnsErrCriterionNotFound() {
	_, err := s.criteria.GetCriterion(s.ctx, uuid.New())
	s.Require().Error(err)
	s.ErrorIs(err, domain.ErrCriterionNotFound)
}

func (s *AcceptanceCriterionSuite) TestUpdateCompletedReturnsErrCriterionNotFoundForStaleID() {
	_, err := s.criteria.UpdateCompleted(s.ctx, uuid.New(), true)
	s.Require().Error(err)
	s.ErrorIs(err, domain.ErrCriterionNotFound)
}

func (s *AcceptanceCriterionSuite) TestUpdateCanceledReturnsErrCriterionNotFoundForStaleID() {
	_, err := s.criteria.UpdateCanceled(s.ctx, uuid.New(), true, "out of scope")
	s.Require().Error(err)
	s.ErrorIs(err, domain.ErrCriterionNotFound)
}
