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

// TaskQuestionSuite covers the task_questions store (migration 169) and the
// analysis_questions resource block/release on board_tasks.
type TaskQuestionSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	tasks  *postgres.BoardTaskStore
	qs     *postgres.TaskQuestionStore
	repoID uuid.UUID
}

func TestTaskQuestionSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(TaskQuestionSuite))
}

func (s *TaskQuestionSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(db)
	s.qs = postgres.NewTaskQuestionStore(db)
	repo, err := postgres.NewRepositoryStore(db).Create(s.ctx, "questions-test", "", "/tmp/questions-test-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *TaskQuestionSuite) TearDownSuite() {
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

func (s *TaskQuestionSuite) newAnalizTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "analiz")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID, TaskNumber: num, Title: "analysis", TaskType: "analiz",
		Column: domain.TaskColumnInProgress, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	return task
}

func (s *TaskQuestionSuite) TestCreateAssignsKeysInOrder() {
	task := s.newAnalizTask()

	first, err := s.qs.Create(s.ctx, domain.TaskQuestion{
		TaskID: task.ID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true,
	})
	s.Require().NoError(err)
	s.Equal("Q1", first.Key)
	s.Equal(domain.QuestionStatusOpen, first.Status)

	second, err := s.qs.Create(s.ctx, domain.TaskQuestion{
		TaskID: task.ID, Prompt: "Keep the old export format?", Kind: domain.QuestionKindProduct,
		RecommendedAnswer: "Keep it for now.",
	})
	s.Require().NoError(err)
	s.Equal("Q2", second.Key)

	all, err := s.qs.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(all, 2)
	s.Equal("Q1", all[0].Key, "ordered by the key's numeric suffix")
	s.Equal("Q2", all[1].Key)

	byKey, err := s.qs.GetByKey(s.ctx, task.ID, "Q1")
	s.Require().NoError(err)
	s.Equal(first.ID, byKey.ID)

	_, err = s.qs.GetByKey(s.ctx, task.ID, "Q9")
	s.ErrorIs(err, domain.ErrQuestionNotFound)
}

func (s *TaskQuestionSuite) TestKeyOrderingIsNumericNotLexical() {
	task := s.newAnalizTask()
	for i := 0; i < 11; i++ {
		_, err := s.qs.Create(s.ctx, domain.TaskQuestion{
			TaskID: task.ID, Prompt: "q", Kind: domain.QuestionKindTechnical, RecommendedAnswer: "a",
		})
		s.Require().NoError(err)
	}
	all, err := s.qs.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(all, 11)
	s.Equal("Q10", all[9].Key, "Q10 sorts after Q9, not lexically before Q2")
	s.Equal("Q11", all[10].Key)
}

func (s *TaskQuestionSuite) TestAnswerAndSubmitLifecycle() {
	task := s.newAnalizTask()
	q, err := s.qs.Create(s.ctx, domain.TaskQuestion{
		TaskID: task.ID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true,
	})
	s.Require().NoError(err)

	now := time.Now().UTC()
	answered, err := domain.ApplyQuestionAnswer(q, "Use SQS.", now)
	s.Require().NoError(err)
	updated, err := s.qs.Update(s.ctx, answered)
	s.Require().NoError(err)
	s.Equal(domain.QuestionStatusAnswered, updated.Status)
	s.Equal("Use SQS.", updated.Answer)
	s.Require().NotNil(updated.AnsweredAt)
	s.Nil(updated.SubmittedAt)

	at := time.Now().UTC().Truncate(time.Millisecond)
	submitted, err := s.qs.MarkSubmitted(s.ctx, task.ID, at)
	s.Require().NoError(err)
	s.Require().Len(submitted, 1)
	s.WithinDuration(at, *submitted[0].SubmittedAt, time.Second)

	again, err := s.qs.MarkSubmitted(s.ctx, task.ID, at)
	s.Require().NoError(err)
	s.Empty(again, "only answered-unsubmitted questions move")

	_, err = s.qs.Get(s.ctx, uuid.New(), q.ID)
	s.ErrorIs(err, domain.ErrQuestionNotFound, "a question is only reachable through its own task")
}

func (s *TaskQuestionSuite) TestCascadesWithItsTask() {
	task := s.newAnalizTask()
	_, err := s.qs.Create(s.ctx, domain.TaskQuestion{
		TaskID: task.ID, Prompt: "q", Kind: domain.QuestionKindTechnical, RecommendedAnswer: "a",
	})
	s.Require().NoError(err)

	s.Require().NoError(s.tasks.Delete(s.ctx, s.repoID, task.ID))

	left, err := s.qs.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Empty(left, "questions go with their task")
}

func (s *TaskQuestionSuite) TestAnalysisQuestionsBlockAndRelease() {
	task := s.newAnalizTask()

	previous, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID, domain.ResourceAnalysisQuestions, "Q1: which queue?")
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnInProgress, previous)

	blocked, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnBlocked, blocked.Column)
	s.Equal(domain.ResourceAnalysisQuestions, blocked.BlockedResource)
	s.Equal(domain.TaskColumnInProgress, blocked.BlockedOriginColumn)

	released, ok, err := s.tasks.ReleaseAnalysisQuestionsBlock(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(domain.TaskColumnInProgress, released.Column)

	after, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnInProgress, after.Column)
	s.Empty(after.BlockedResource)
	s.Empty(string(after.BlockedOriginColumn))

	_, ok, err = s.tasks.ReleaseAnalysisQuestionsBlock(s.ctx, task.ID)
	s.Require().NoError(err)
	s.False(ok, "a task not blocked on this resource is left alone")
}
