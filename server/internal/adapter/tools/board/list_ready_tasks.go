package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const listReadyTasksToolName = "list_ready_tasks"

type listReadyTasksArgs struct {
	Column       string `json:"column"`
	AssignedToMe bool   `json:"assigned_to_me"`
}

type listReadyTasksTool struct {
	kit *ToolKit
}

func newListReadyTasksTool(kit *ToolKit) port.ToolExecutor {
	return &listReadyTasksTool{kit: kit}
}

func (t *listReadyTasksTool) Name() string {
	return listReadyTasksToolName
}

func (t *listReadyTasksTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listReadyTasksToolName,
			Description: "List the unblocked queue: backlog and todo tasks with no " +
				"unfinished blocked_by blocker, sorted by priority (critical first) " +
				"then age. Use this instead of scanning list_board_tasks to decide " +
				"what to pick up next — a task listed here can be claimed with " +
				"claim_board_task and moved to in_progress. Tasks parked in the " +
				"blocked column, or waiting on an unfinished blocker, never appear.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"column": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"backlog", "todo"},
						"description": "Optional column filter. Defaults to both backlog and todo.",
					},
					"assigned_to_me": map[string]interface{}{
						"type":        "boolean",
						"description": "Only return tasks already assigned to the calling agent.",
					},
				},
			},
		},
	}
}

func (t *listReadyTasksTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args listReadyTasksArgs
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(listReadyTasksToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	var column domain.TaskColumn
	switch args.Column {
	case "":
	case string(domain.TaskColumnBacklog), string(domain.TaskColumnTodo):
		column = domain.TaskColumn(args.Column)
	default:
		return toolError(listReadyTasksToolName, fmt.Sprintf("invalid column %q: must be %q or %q", args.Column, domain.TaskColumnBacklog, domain.TaskColumnTodo))
	}

	var agentID uuid.UUID
	if args.AssignedToMe {
		agentID = registry.AgentIDFromContext(ctx)
		if agentID == uuid.Nil {
			return toolError(listReadyTasksToolName, "assigned_to_me was requested but this run has no agent identity")
		}
	}

	tasks, err := t.kit.listReadyTasks(ctx)
	if err != nil {
		return toolError(listReadyTasksToolName, err.Error())
	}

	filtered := make([]domain.BoardTask, 0, len(tasks))
	for _, task := range tasks {
		if column != "" && task.Column != column {
			continue
		}
		if args.AssignedToMe && (task.AssigneeAgentID == nil || *task.AssigneeAgentID != agentID) {
			continue
		}
		filtered = append(filtered, task)
	}
	return toolJSON(listReadyTasksToolName, map[string]any{"tasks": filtered, "count": len(filtered)})
}
