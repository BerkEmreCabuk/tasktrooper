package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const addTaskCommentToolName = "add_task_comment"

type addCommentArgs struct {
	TaskID  string `json:"task_id"`
	Content string `json:"content"`
}

type addCommentTool struct {
	kit *ToolKit
}

func newAddCommentTool(kit *ToolKit) port.ToolExecutor {
	return &addCommentTool{kit: kit}
}

func (t *addCommentTool) Name() string {
	return addTaskCommentToolName
}

func (t *addCommentTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: addTaskCommentToolName,
			Description: "Add a comment to a board task as the current agent. " +
				"Comment when something needs a person or the next agent to ACT: a rejection and why, a failure with its expected-vs-actual, a blocker, a question you cannot answer yourself, work you did not do. " +
				"Do NOT comment to report that things went well — a passed review, a green build, a successful merge or deploy, a criterion you approved, \"moving to X\". " +
				"The column, the criteria, the pull request, the pipeline result and the task history already carry all of that, and a thread of confirmations buries the one comment that mattered. " +
				"Never paste the pull request link or number either: it is a field on the task and the board renders it.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis).",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Comment text",
					},
				},
				"required": []string{"task_id", "content"},
			},
		},
	}
}

func (t *addCommentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args addCommentArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(addTaskCommentToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(addTaskCommentToolName, err.Error())
	}
	if args.Content == "" {
		return toolError(addTaskCommentToolName, "content is required")
	}
	agentID, err := resolveAgentID(ctx)
	if err != nil {
		return toolError(addTaskCommentToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(addTaskCommentToolName, err.Error())
	}
	comment, err := t.kit.Tasks.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
		Content:    args.Content,
		AuthorType: "agent",
		AuthorID:   agentID.String(),
	})
	if err != nil {
		return toolError(addTaskCommentToolName, err.Error())
	}
	return toolJSON(addTaskCommentToolName, comment)
}
