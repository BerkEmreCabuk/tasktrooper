package board

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	listProjectsToolName     = "list_projects"
	listRepositoriesToolName = "list_repositories"
)

type listProjectsTool struct {
	kit *ToolKit
}

func newListProjectsTool(kit *ToolKit) port.ToolExecutor {
	return &listProjectsTool{kit: kit}
}

func (t *listProjectsTool) Name() string {
	return listProjectsToolName
}

func (t *listProjectsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        listProjectsToolName,
			Description: "List the projects on the board (id, name, description). Use to answer how many projects exist or what they are.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]interface{}{},
			},
		},
	}
}

func (t *listProjectsTool) Execute(ctx context.Context, _ string) domain.ToolResult {
	projects, err := t.kit.Workspace.ListProjects(ctx)
	if err != nil {
		return toolError(listProjectsToolName, err.Error())
	}
	out := make([]map[string]any, 0, len(projects))
	for _, p := range projects {
		out = append(out, map[string]any{
			"id":          p.ID.String(),
			"name":        p.Name,
			"description": p.Description,
		})
	}
	return toolJSON(listProjectsToolName, map[string]any{"count": len(out), "projects": out})
}

type listRepositoriesTool struct {
	kit *ToolKit
}

func newListRepositoriesTool(kit *ToolKit) port.ToolExecutor {
	return &listRepositoriesTool{kit: kit}
}

func (t *listRepositoriesTool) Name() string {
	return listRepositoriesToolName
}

func (t *listRepositoriesTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        listRepositoriesToolName,
			Description: "List the code repositories on the board (id, name, description, root_path, kind, remote_url, which projects they belong to). Use to answer what repositories exist and where a repository's code lives — never ask the user for a repository path or URL before calling this.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties":           map[string]interface{}{},
			},
		},
	}
}

func (t *listRepositoriesTool) Execute(ctx context.Context, _ string) domain.ToolResult {
	repos, err := t.kit.Workspace.ListRepositories(ctx)
	if err != nil {
		return toolError(listRepositoriesToolName, err.Error())
	}
	out := make([]map[string]any, 0, len(repos))
	for _, r := range repos {
		projectIDs := make([]string, 0, len(r.ProjectIDs))
		for _, id := range r.ProjectIDs {
			projectIDs = append(projectIDs, id.String())
		}
		entry := map[string]any{
			"id":          r.ID.String(),
			"name":        r.Name,
			"description": r.Description,
			"project_ids": projectIDs,
			// root_path lets an agent match the repo it was handed against the
			// workspace it is running in instead of asking the human where the
			// code lives.
			"root_path": r.RootPath,
			"kind":      r.Kind,
		}
		if r.RemoteURL != "" {
			entry["remote_url"] = r.RemoteURL
		}
		out = append(out, entry)
	}
	return toolJSON(listRepositoriesToolName, map[string]any{"count": len(out), "repositories": out})
}
