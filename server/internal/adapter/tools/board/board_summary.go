package board

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const getBoardSummaryToolName = "get_board_summary"

type boardSummaryTool struct {
	kit *ToolKit
}

func newBoardSummaryTool(kit *ToolKit) port.ToolExecutor {
	return &boardSummaryTool{kit: kit}
}

func (t *boardSummaryTool) Name() string { return getBoardSummaryToolName }

func (t *boardSummaryTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        getBoardSummaryToolName,
			Description: "Get an aggregated status snapshot of the board: total task count and counts grouped by column, type, and priority. Use to report status without listing every task.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]interface{}{},
			},
		},
	}
}

func (t *boardSummaryTool) Execute(ctx context.Context, _ string) domain.ToolResult {
	tasks, err := t.kit.listBoardTasks(ctx)
	if err != nil {
		return toolError(getBoardSummaryToolName, err.Error())
	}
	byColumn := map[string]int{}
	byType := map[string]int{}
	byPriority := map[string]int{}
	for _, task := range tasks {
		byColumn[string(task.Column)]++
		byType[string(task.TaskType)]++
		byPriority[string(task.Priority)]++
	}
	return toolJSON(getBoardSummaryToolName, map[string]any{
		"total":       len(tasks),
		"by_column":   byColumn,
		"by_type":     byType,
		"by_priority": byPriority,
	})
}
