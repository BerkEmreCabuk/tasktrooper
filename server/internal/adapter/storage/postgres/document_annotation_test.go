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

// DocumentAnnotationSuite covers the task_documents format column and the
// task_document_annotations store (migration 164).
type DocumentAnnotationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	tasks  *postgres.BoardTaskStore
	docs   *postgres.TaskDocumentStore
	anns   *postgres.TaskDocumentAnnotationStore
	repoID uuid.UUID
}

func TestDocumentAnnotationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DocumentAnnotationSuite))
}

func (s *DocumentAnnotationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(db)
	s.docs = postgres.NewTaskDocumentStore(db)
	s.anns = postgres.NewTaskDocumentAnnotationStore(db)
	repo, err := postgres.NewRepositoryStore(db).Create(s.ctx, "annotations-test", "", "/tmp/annotations-test-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *DocumentAnnotationSuite) TearDownSuite() {
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

func (s *DocumentAnnotationSuite) newTaskWithDocument(format domain.DocumentFormat) (domain.BoardTask, domain.TaskDocument) {
	num, err := s.tasks.NextTaskNumber(s.ctx, "analiz")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID, TaskNumber: num, Title: "analysis", TaskType: "analiz",
		Column: domain.TaskColumnAnalizReview, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	doc, err := s.docs.Create(s.ctx, domain.TaskDocument{
		TaskID: task.ID, Title: "analiz: report", Content: "<p>x</p>", Format: format, CreatedByType: "agent",
	})
	s.Require().NoError(err)
	return task, doc
}

func (s *DocumentAnnotationSuite) TestDocumentFormatRoundTrips() {
	task, html := s.newTaskWithDocument(domain.DocumentFormatHTML)
	s.Equal(domain.DocumentFormatHTML, html.Format)

	md, err := s.docs.Create(s.ctx, domain.TaskDocument{TaskID: task.ID, Title: "notes", Content: "# x", CreatedByType: "user"})
	s.Require().NoError(err)
	s.Equal(domain.DocumentFormatMarkdown, md.Format, "an unset format is stored as markdown")

	md.Format = domain.DocumentFormatHTML
	updated, err := s.docs.Update(s.ctx, md)
	s.Require().NoError(err)
	s.Equal(domain.DocumentFormatHTML, updated.Format)

	listed, err := s.docs.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Len(listed, 2)

	_, err = s.docs.Get(s.ctx, task.ID, uuid.New())
	s.ErrorIs(err, domain.ErrTaskDocumentNotFound)
}

func (s *DocumentAnnotationSuite) TestAnnotationLifecycle() {
	task, doc := s.newTaskWithDocument(domain.DocumentFormatHTML)

	created, err := s.anns.Create(s.ctx, domain.TaskDocumentAnnotation{
		TaskID: task.ID, DocumentID: doc.ID, Quote: "Use a queue.", Prefix: "Plan: ", Suffix: " Then", Body: "why?",
	})
	s.Require().NoError(err)
	s.Equal(domain.AnnotationStatusOpen, created.Status)
	s.Equal("user", created.CreatedByType)
	s.Nil(created.SubmittedAt)
	s.Nil(created.ResolvedAt)

	second, err := s.anns.Create(s.ctx, domain.TaskDocumentAnnotation{
		TaskID: task.ID, DocumentID: doc.ID, Quote: "x", Body: "second",
	})
	s.Require().NoError(err)

	all, err := s.anns.ListByTask(s.ctx, task.ID, nil)
	s.Require().NoError(err)
	s.Require().Len(all, 2)
	s.Equal(created.ID, all[0].ID, "ordered by created_at")

	byDoc, err := s.anns.ListByTask(s.ctx, task.ID, &doc.ID)
	s.Require().NoError(err)
	s.Len(byDoc, 2)
	other := uuid.New()
	none, err := s.anns.ListByTask(s.ctx, task.ID, &other)
	s.Require().NoError(err)
	s.NotNil(none)
	s.Empty(none)

	at := time.Now().UTC().Truncate(time.Millisecond)
	moved, err := s.anns.MarkSubmitted(s.ctx, task.ID, []uuid.UUID{created.ID, second.ID}, at)
	s.Require().NoError(err)
	s.ElementsMatch([]uuid.UUID{created.ID, second.ID}, moved)
	again, err := s.anns.MarkSubmitted(s.ctx, task.ID, []uuid.UUID{created.ID}, at)
	s.Require().NoError(err)
	s.Empty(again, "only open comments move")

	got, err := s.anns.Get(s.ctx, task.ID, created.ID)
	s.Require().NoError(err)
	s.Equal(domain.AnnotationStatusSubmitted, got.Status)
	s.Require().NotNil(got.SubmittedAt)
	s.WithinDuration(at, *got.SubmittedAt, time.Second)

	resolvedAt := time.Now().UTC()
	got.Status = domain.AnnotationStatusResolved
	got.Reply = "switched to cron"
	got.ResolvedAt = &resolvedAt
	updated, err := s.anns.Update(s.ctx, got)
	s.Require().NoError(err)
	s.Equal(domain.AnnotationStatusResolved, updated.Status)
	s.Equal("switched to cron", updated.Reply)
	s.NotNil(updated.ResolvedAt)

	_, err = s.anns.Get(s.ctx, uuid.New(), created.ID)
	s.ErrorIs(err, domain.ErrAnnotationNotFound, "an annotation is only reachable through its own task")

	s.Require().NoError(s.anns.Delete(s.ctx, task.ID, second.ID))
	s.ErrorIs(s.anns.Delete(s.ctx, task.ID, second.ID), domain.ErrAnnotationNotFound)

	s.Require().NoError(s.docs.Delete(s.ctx, task.ID, doc.ID))
	left, err := s.anns.ListByTask(s.ctx, task.ID, nil)
	s.Require().NoError(err)
	s.Empty(left, "annotations go with their document")
}
