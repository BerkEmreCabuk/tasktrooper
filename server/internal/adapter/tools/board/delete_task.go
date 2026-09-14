package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const deleteBoardTaskToolName = "delete_board_task"

// deletableColumns are the columns a task may be removed from without asking
// twice. Both are planning columns: nothing has been built for the task yet, so
// deleting it destroys a plan and nothing else. Past them the task carries a
// branch, comments, criteria verdicts and a pipeline history — the record of
// work that actually happened — and the right move is almost always to move it
// somewhere terminal instead of erasing it.
var deletableColumns = map[domain.TaskColumn]bool{
	domain.TaskColumnBacklog: true,
	domain.TaskColumnTodo:    true,
}

type deleteTaskArgs struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
	// Force removes a task that has already left the planning columns. It is a
	// deliberate second step, not a default.
	Force bool `json:"force"`
}

type deleteTaskTool struct {
	kit *ToolKit
}

func newDeleteTaskTool(kit *ToolKit) port.ToolExecutor {
	return &deleteTaskTool{kit: kit}
}

func (t *deleteTaskTool) Name() string {
	return deleteBoardTaskToolName
}

func (t *deleteTaskTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: deleteBoardTaskToolName,
			Description: "Delete a board task permanently, with everything on it: comments, acceptance criteria, documents and relations. There is no undo. " +
				"Use it when the user asks for a task to be removed — a duplicate, a task opened by mistake, or work that was merged into another task. " +
				"When several tasks are being merged into one, delete the ones that were merged away instead of leaving them open. " +
				"A task that has left backlog/todo is refused unless force=true: past those columns it carries a branch and a work history, and cancelled work belongs in a terminal column, not erased.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis).",
					},
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Why this task is being deleted (e.g. \"merged into DE-4\"). Recorded in the run trace — the task itself is gone, so this is the only trace left.",
					},
					"force": map[string]interface{}{
						"type":        "boolean",
						"description": "Delete a task that is no longer in backlog or todo. Only set this after the user has asked for that specific task to be deleted.",
					},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

func (t *deleteTaskTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args deleteTaskArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deleteBoardTaskToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(deleteBoardTaskToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(deleteBoardTaskToolName, err.Error())
	}
	// The record is read before it is removed: what comes back is the only
	// description of the task that will exist afterwards, and the ledger entry
	// the chat renders is built from it.
	task, found := t.kit.findTask(ctx, taskID)
	if !found {
		return toolError(deleteBoardTaskToolName, fmt.Sprintf("task %s not found on the board", args.TaskID))
	}
	if !deletableColumns[task.Column] && !args.Force {
		return toolJSON(deleteBoardTaskToolName, map[string]any{
			"deleted": false,
			"reason":  fmt.Sprintf("task is in %s, not a planning column", task.Column),
			"task":    task,
			"hint": "Work has already started on this task, and deleting it would erase its branch history, comments and criteria. " +
				"Move it to a terminal column with move_board_task instead, or call this again with force=true if the user specifically asked for THIS task to be deleted.",
		})
	}
	if err := t.kit.Tasks.DeleteTask(ctx, repositoryID, taskID); err != nil {
		return toolError(deleteBoardTaskToolName, err.Error())
	}
	return toolJSON(deleteBoardTaskToolName, map[string]any{
		"deleted":       true,
		"id":            task.ID,
		"key":           task.Key,
		"title":         task.Title,
		"column":        task.Column,
		"priority":      task.Priority,
		"repository_id": task.RepositoryID,
		"delete_reason": args.Reason,
	})
}
