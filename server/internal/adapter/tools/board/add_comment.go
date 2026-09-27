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
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type": "string",
					},
					"content": map[string]interface{}{
						"type": "string",
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
