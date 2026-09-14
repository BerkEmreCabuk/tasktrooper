package board_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeBlockedTaker models the store's take-once semantics: the parked task is
// handed back exactly once, mirroring the atomic UPDATE ... RETURNING that
// clears blocked_session_id in the same statement.
type fakeBlockedTaker struct {
	task  domain.BoardTask
	on    uuid.UUID
	taken bool
	err   error
	calls int
}

func (f *fakeBlockedTaker) TakeBlockedBySession(_ context.Context, sessionID uuid.UUID) (domain.BoardTask, bool, error) {
	f.calls++
	if f.err != nil {
		return domain.BoardTask{}, false, f.err
	}
	if f.taken || sessionID != f.on {
		return domain.BoardTask{}, false, nil
	}
	f.taken = true
	return f.task, true, nil
}

// fakeTaskCommenter records what the resumer writes onto the task.
type fakeTaskCommenter struct {
	comments []domain.CreateTaskCommentRequest
	err      error
}

func (f *fakeTaskCommenter) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	if f.err != nil {
		return domain.TaskComment{}, f.err
	}
	f.comments = append(f.comments, req)
	return domain.TaskComment{Content: req.Content}, nil
}

type ResumeSuite struct {
	suite.Suite
	board     *fakeBoardConfigStore
	events    *fakeEventStore
	runs      *fakeRunStore
	runner    *fakeRunner
	disp      *board.Dispatcher
	taker     *fakeBlockedTaker
	commenter *fakeTaskCommenter
	resumer   *board.AnswerResumer
	session   uuid.UUID
	taskID    uuid.UUID
	repoID    uuid.UUID
	question  string
}

func TestResumeSuite(t *testing.T) {
	suite.Run(t, new(ResumeSuite))
}

func (s *ResumeSuite) SetupTest() {
	agentA := uuid.New()
	s.board = &fakeBoardConfigStore{
		agentsByColumn: map[string][]uuid.UUID{"todo": {agentA}},
	}
	s.events = &fakeEventStore{}
	s.runs = &fakeRunStore{}
	s.runner = &fakeRunner{}
	s.disp = board.NewDispatcher(s.board, s.events, s.runs, s.runner, true)

	s.session = uuid.New()
	s.taskID = uuid.New()
	s.repoID = uuid.New()
	s.question = "Which database should the new service use?"
	// The store restores the origin column before handing the task back, so the
	// resumed task arrives in the column it was working in — not in 'blocked'.
	s.taker = &fakeBlockedTaker{
		on: s.session,
		task: domain.BoardTask{
			ID:                  s.taskID,
			RepositoryID:        s.repoID,
			Title:               "Add billing service",
			Column:              domain.TaskColumnTodo,
			BlockedOriginColumn: domain.TaskColumnTodo,
			BlockedQuestion:     s.question,
		},
	}
	s.commenter = &fakeTaskCommenter{}
	s.resumer = board.NewAnswerResumer(s.taker, s.disp, s.commenter)
}

// The whole point of parking a task: answering re-dispatches it, and the agent
// gets both halves of the exchange so it can continue instead of restarting.
func (s *ResumeSuite) TestAnswerRedispatchesWithQuestionAndAnswer() {
	s.True(s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres"))

	s.Require().Len(s.runner.jobs, 1)
	s.Equal(s.taskID, s.runner.jobs[0].Task.ID)

	s.Require().Len(s.events.events, 1)
	var payload map[string]any
	s.Require().NoError(json.Unmarshal(s.events.events[0].Payload, &payload))
	s.Equal("question_answered", payload["resumed"])
	s.Equal(s.question, payload["question"])
	s.Equal("Postgres", payload["answer"])
}

// The answer has to outlive the run it resumes. Without a record on the task,
// the next run (retry, revision, verification) starts blind and asks the human
// the question they already answered.
func (s *ResumeSuite) TestAnswerIsRecordedOnTheTask() {
	s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")

	s.Require().Len(s.commenter.comments, 1)
	comment := s.commenter.comments[0]
	s.Equal("system", comment.AuthorType)
	s.True(prompt.IsClarificationComment(comment.Content))
	s.Contains(comment.Content, s.question)
	s.Contains(comment.Content, "Postgres")

	// And a later run reads it back as an answered requirement.
	replay := prompt.AnsweredClarificationsMessage([]domain.TaskComment{{Content: comment.Content}})
	s.Contains(replay, s.question)
	s.Contains(replay, "Postgres")
}

// Losing the comment must not cost the resume: the run being dispatched still
// carries both halves in its payload.
func (s *ResumeSuite) TestCommentFailureStillResumes() {
	s.commenter.err = errors.New("db down")

	s.True(s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres"))
	s.Len(s.runner.jobs, 1)
}

// Every human message in every session hits this path, so a session with no
// task parked on it must cost one lookup and nothing else.
func (s *ResumeSuite) TestUnrelatedSessionIsANoOp() {
	s.False(s.resumer.ResumeOnAnswer(context.Background(), uuid.New(), "hello"))

	s.Equal(1, s.taker.calls)
	s.Empty(s.runner.jobs)
	s.Empty(s.events.events)
	s.Empty(s.commenter.comments)
}

// A follow-up message in the same chat must not fire a second run for a task
// that is already back in flight.
func (s *ResumeSuite) TestSecondAnswerDoesNotDispatchTwice() {
	s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	s.resumer.ResumeOnAnswer(context.Background(), s.session, "...and use pgx")

	s.Len(s.runner.jobs, 1, "the take clears the block, so only the first answer resumes the task")
}

// A store error must not take down the chat request that triggered the lookup.
func (s *ResumeSuite) TestStoreErrorIsSwallowed() {
	s.taker.err = errors.New("db down")

	s.NotPanics(func() {
		s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	})
	s.Empty(s.runner.jobs)
}

// Resume is wired in optionally (desktop builds skip the board); the zero value
// and a nil resumer must both stay inert rather than panic on a chat message.
func (s *ResumeSuite) TestNilResumerIsInert() {
	var nilResumer *board.AnswerResumer
	s.NotPanics(func() {
		nilResumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	})

	partial := board.NewAnswerResumer(nil, s.disp, nil)
	s.NotPanics(func() {
		partial.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	})
	s.Empty(s.runner.jobs)
}
