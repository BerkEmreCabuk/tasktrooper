package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const listBoardTasksToolName = "list_board_tasks"

type listTasksArgs struct {
	Column string `json:"column"`
}

type listTasksTool struct {
	kit *ToolKit
}

func newListTasksTool(kit *ToolKit) port.ToolExecutor {
	return &listTasksTool{kit: kit}
}

func (t *listTasksTool) Name() string {
	return listBoardTasksToolName
}

func (t *listTasksTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        listBoardTasksToolName,
			Description: "List board tasks for the current repository or team context.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"column": map[string]interface{}{
						"type":        "string",
						"description": "Optional column filter",
					},
				},
			},
		},
	}
}

func (t *listTasksTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args listTasksArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(listBoardTasksToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	tasks, err := t.kit.listBoardTasks(ctx)
	if err != nil {
		return toolError(listBoardTasksToolName, err.Error())
	}
	if args.Column != "" {
		col := domain.TaskColumn(args.Column)
		filtered := make([]domain.BoardTask, 0)
		for _, task := range tasks {
			if task.Column == col {
				filtered = append(filtered, task)
			}
		}
		tasks = filtered
	}
	return toolJSON(listBoardTasksToolName, map[string]any{"tasks": tasks})
}
