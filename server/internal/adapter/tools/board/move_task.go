package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const moveBoardTaskToolName = "move_board_task"

type moveTaskArgs struct {
	TaskID string `json:"task_id"`
	Column string `json:"column"`
}

type moveTaskTool struct {
	kit *ToolKit
}

func newMoveTaskTool(kit *ToolKit) port.ToolExecutor {
	return &moveTaskTool{kit: kit}
}

func (t *moveTaskTool) Name() string {
	return moveBoardTaskToolName
}

func (t *moveTaskTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        moveBoardTaskToolName,
			Description: "Move a board task to another column.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis).",
					},
					"column": map[string]interface{}{
						"type":        "string",
						"description": "Target column slug",
					},
				},
				"required": []string{"task_id", "column"},
			},
		},
	}
}

func (t *moveTaskTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args moveTaskArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(moveBoardTaskToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(moveBoardTaskToolName, err.Error())
	}
	col := domain.TaskColumn(args.Column)
	if !domain.ValidTaskColumn(col) {
		return toolError(moveBoardTaskToolName, fmt.Sprintf("invalid column: %s", args.Column))
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(moveBoardTaskToolName, err.Error())
	}
	req := domain.UpdateBoardTaskRequest{
		Column: &col,
		Actor:  domain.TaskActorAgent,
	}
	if actorID, actorErr := resolveAgentID(ctx); actorErr == nil {
		req.ActorAgentID = &actorID
	}
	task, err := t.kit.Tasks.UpdateTask(ctx, repositoryID, taskID, req)
	if err != nil {
		return toolError(moveBoardTaskToolName, err.Error())
	}
	return toolJSON(moveBoardTaskToolName, task)
}
