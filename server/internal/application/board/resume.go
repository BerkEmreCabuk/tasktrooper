package board

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// BlockedTaskTaker hands back (and atomically clears) the task parked on a
// clarification session, so an answer resumes exactly one task exactly once.
type BlockedTaskTaker interface {
	TakeBlockedBySession(ctx context.Context, sessionID uuid.UUID) (domain.BoardTask, bool, error)
}

// TaskCommenter records the answered exchange on the task itself. Nil-safe.
type TaskCommenter interface {
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

// AnswerResumer closes the loop on an agent that got stuck: the agent asked a
// question and its task was parked (runner.openClarificationChat), the human
// answered in that chat, and the task now goes back into the dispatch queue
// with the question and answer attached so work continues where it stopped.
//
// The exchange is also written onto the task as a comment. The dispatch payload
// only reaches the one run it triggers; the comment is what every later run
// (retry, revision, verification) reads, and without it those runs came back to
// the human with a question that had already been answered.
type AnswerResumer struct {
	tasks      BlockedTaskTaker
	dispatcher *Dispatcher
	comments   TaskCommenter
}

func NewAnswerResumer(tasks BlockedTaskTaker, dispatcher *Dispatcher, comments TaskCommenter) *AnswerResumer {
	return &AnswerResumer{tasks: tasks, dispatcher: dispatcher, comments: comments}
}

// ResumeOnAnswer is called for every human message in any session; sessions
// with no task parked on them are a cheap no-op lookup.
//
// It reports whether this message answered a parked question. The caller uses
// that to stay out of the way: the clarification chat holds nothing but the
// question and the reply, so letting its own agent take a turn on the answer
// produced a context-free model that could only ask the same question again.
// The re-dispatched task run is the one that owns the answer.
func (r *AnswerResumer) ResumeOnAnswer(ctx context.Context, sessionID uuid.UUID, answer string) bool {
	if r == nil || r.tasks == nil || r.dispatcher == nil {
		return false
	}
	task, ok, err := r.tasks.TakeBlockedBySession(ctx, sessionID)
	if err != nil {
		log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("resume: blocked task lookup failed")
		return false
	}
	if !ok {
		return false
	}
	// The question is cleared by the take, so carry it in the payload — the
	// agent needs both halves to make sense of the answer.
	r.recordAnswer(ctx, task, answer)
	if err := r.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":  "question_answered",
			"question": task.BlockedQuestion,
			"answer":   answer,
			// The resume is the control plane's move, not the human's. Without
			// these keys board history rendered it as an empty "Moved by User"
			// row — the human was shown a move they never made.
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonQuestionAnswered,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("resume: redispatch failed")
		return true
	}
	log.Info().Str("task_id", task.ID.String()).Msg("blocked task resumed after answer")
	return true
}

// recordAnswer persists the exchange on the task. A failure here costs the
// task its memory of this answer, not the resume itself — the run about to be
// dispatched still receives both halves in its payload.
func (r *AnswerResumer) recordAnswer(ctx context.Context, task domain.BoardTask, answer string) {
	if r.comments == nil || strings.TrimSpace(answer) == "" {
		return
	}
	content := prompt.ClarificationAnswerComment(task.BlockedQuestion, answer)
	if _, err := r.comments.AddComment(ctx, task.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    content,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("resume: recording the answer on the task failed")
	}
}
