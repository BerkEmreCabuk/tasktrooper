package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const listTaskCommentsToolName = "list_task_comments"

// listCommentsTool is the read half of the task conversation.
//
// It exists because its absence was actively harmful: the kit shipped
// add_task_comment with no counterpart, so a run told to "read the reviewer's
// feedback" looked for a comment tool, found the only one there was, and POSTED
// a comment instead of reading any — the model announced "let me check the task
// comments" and then wrote one. Naming the read explicitly is what turns that
// instruction into a call that can succeed.
type listCommentsTool struct {
	kit *ToolKit
}

type listCommentsArgs struct {
	TaskID string `json:"task_id"`
	Limit  int    `json:"limit"`
}

// CommentLister is the read side of TaskManager's comments, kept separate so a
// build whose task service predates it simply registers no tool rather than
// failing to compile.
type CommentLister interface {
	ListComments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskComment, error)
}

func newListCommentsTool(kit *ToolKit) port.ToolExecutor {
	return &listCommentsTool{kit: kit}
}

func (t *listCommentsTool) Name() string { return listTaskCommentsToolName }

func (t *listCommentsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listTaskCommentsToolName,
			Description: "READ the comments on a board task — the reviewer's revision feedback, the human's answers and every earlier agent's notes, oldest first. " +
				"Use it before starting work on a task that came back from review, and whenever you are about to say \"let me check the comments\". " +
				"This tool only reads; add_task_comment is what writes one. Reviewer notes left on the PULL REQUEST are not here — get_task_pull_request returns those.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "How many of the most recent comments to return (default 20).",
					},
				},
			},
		},
	}
}

func (t *listCommentsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args listCommentsArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(listTaskCommentsToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	taskID, err := t.kit.resolveTaskArg(ctx, args.TaskID)
	if err != nil {
		return toolError(listTaskCommentsToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(listTaskCommentsToolName, err.Error())
	}
	lister, ok := t.kit.Tasks.(CommentLister)
	if !ok {
		return toolError(listTaskCommentsToolName, "this build cannot read task comments")
	}
	comments, err := lister.ListComments(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(listTaskCommentsToolName, err.Error())
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}
	// Newest are what a revision run needs; the tail is the newest because the
	// service returns them oldest first, and the slice keeps that order so the
	// conversation still reads forwards.
	if len(comments) > limit {
		comments = comments[len(comments)-limit:]
	}
	return toolJSON(listTaskCommentsToolName, map[string]interface{}{
		"task_id":  taskID.String(),
		"count":    len(comments),
		"comments": comments,
	})
}
