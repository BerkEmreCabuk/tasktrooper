package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (h *Handler) registerQuestionRoutes(app fiber.Router) {
	app.Get("/v1/repositories/:id/tasks/:taskId/questions", h.ListTaskQuestions)
	app.Patch("/v1/repositories/:id/tasks/:taskId/questions/:questionId", h.UpdateTaskQuestion)
	app.Post("/v1/repositories/:id/tasks/:taskId/questions/submit", h.SubmitTaskQuestions)
}

// questionError maps the open-questions flow's failures the same way
// annotationError does: a missing task/question (404), a request the
// current state refuses (409), a malformed one (400), or every pending
// blocking question not yet answered (422).
func questionError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrBoardTaskNotFound), errors.Is(err, domain.ErrQuestionNotFound):
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	case errors.Is(err, domain.ErrQuestionConflict):
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "conflict"},
		})
	case errors.Is(err, domain.ErrPendingBlockingQuestions):
		return c.Status(fiber.StatusUnprocessableEntity).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "unprocessable_entity"},
		})
	case errors.Is(err, domain.ErrQuestionInvalid):
		return badRequest(c, err.Error())
	}
	return badRequestErr(c, err)
}

// ListTaskQuestions — GET /v1/repositories/:id/tasks/:taskId/questions
//
// Ordered by key (Q1, Q2, … Q10); withdrawn ones are included, the UI hides
// them.
func (h *Handler) ListTaskQuestions(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	items, err := h.repositorySvc.ListQuestions(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		return questionError(c, err)
	}
	if items == nil {
		items = []domain.TaskQuestion{}
	}
	return c.JSON(fiber.Map{"questions": items})
}

type updateTaskQuestionRequest struct {
	Answer string `json:"answer"`
}

// UpdateTaskQuestion — PATCH /v1/repositories/:id/tasks/:taskId/questions/:questionId
//
// Saves the human's answer as typed. Allowed only while the task is blocked
// on analysis_questions or in analiz_review; refuses a withdrawn question.
func (h *Handler) UpdateTaskQuestion(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	questionID, err := uuid.Parse(c.Params("questionId"))
	if err != nil {
		return badRequest(c, "invalid question id")
	}
	var req updateTaskQuestionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	q, err := h.repositorySvc.AnswerQuestion(h.enrichContext(c), repositoryID, taskID, questionID, req.Answer)
	if err != nil {
		return questionError(c, err)
	}
	return c.JSON(fiber.Map{"question": q})
}

// SubmitTaskQuestions — POST /v1/repositories/:id/tasks/:taskId/questions/submit
//
// The human's "send answers": every answered-unsubmitted question is told to
// the agent, the block clears, the task returns to the column it came from
// (blocked_origin_column, falling back to in_progress) and the analyst is
// re-dispatched. 422 if a blocking question is still unanswered.
func (h *Handler) SubmitTaskQuestions(c *fiber.Ctx) error {
	repositoryID, taskID, err := parseRepositoryTaskParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}
	task, submitted, err := h.repositorySvc.SubmitQuestions(h.enrichContext(c), repositoryID, taskID)
	if err != nil {
		return questionError(c, err)
	}
	return c.JSON(fiber.Map{"task": task, "submitted": submitted})
}
