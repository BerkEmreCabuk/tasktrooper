package board

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const listTeamToolName = "list_team"

type listTeamTool struct {
	kit *ToolKit
}

func newListTeamTool(kit *ToolKit) port.ToolExecutor {
	return &listTeamTool{kit: kit}
}

func (t *listTeamTool) Name() string {
	return listTeamToolName
}

func (t *listTeamTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        listTeamToolName,
			Description: "List the orchestration team: each agent's name, role description, and whether it is enabled. Use to see who is on the team and which role handles which kind of work before assigning or decomposing.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]interface{}{},
			},
		},
	}
}

func (t *listTeamTool) Execute(ctx context.Context, _ string) domain.ToolResult {
	agents, err := t.kit.Team.ListAgents(ctx)
	if err != nil {
		return toolError(listTeamToolName, err.Error())
	}
	out := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		out = append(out, map[string]any{
			"name":          a.Name,
			"role":          a.Description,
			"subagent_type": a.SubagentType,
			"enabled":       a.Enabled,
		})
	}
	return toolJSON(listTeamToolName, map[string]any{"count": len(out), "team": out})
}
