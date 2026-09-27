package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	createProjectToolName         = "create_project"
	updateProjectToolName         = "update_project"
	setRepositoryProjectsToolName = "set_repository_projects"
)

type createProjectArgs struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type createProjectTool struct {
	kit *ToolKit
}

func newCreateProjectTool(kit *ToolKit) port.ToolExecutor {
	return &createProjectTool{kit: kit}
}

func (t *createProjectTool) Name() string { return createProjectToolName }

func (t *createProjectTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: createProjectToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type": "string",
					},
					"description": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

func (t *createProjectTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	if t.kit.Workspace == nil {
		return toolError(createProjectToolName, "workspace is not available")
	}
	var args createProjectArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(createProjectToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Name) == "" {
		return toolError(createProjectToolName, "name is required")
	}
	project, err := t.kit.Workspace.CreateProject(ctx, args.Name, args.Description)
	if err != nil {
		return toolError(createProjectToolName, err.Error())
	}
	return toolJSON(createProjectToolName, project)
}

type updateProjectArgs struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateProjectTool struct {
	kit *ToolKit
}

func newUpdateProjectTool(kit *ToolKit) port.ToolExecutor {
	return &updateProjectTool{kit: kit}
}

func (t *updateProjectTool) Name() string { return updateProjectToolName }

func (t *updateProjectTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: updateProjectToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"project_id": map[string]interface{}{
						"type": "string",
					},
					"name": map[string]interface{}{
						"type": "string",
					},
					"description": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"project_id"},
			},
		},
	}
}

func (t *updateProjectTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	if t.kit.Workspace == nil {
		return toolError(updateProjectToolName, "workspace is not available")
	}
	var args updateProjectArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(updateProjectToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	projectID, err := uuid.Parse(strings.TrimSpace(args.ProjectID))
	if err != nil {
		return toolError(updateProjectToolName, "invalid project_id")
	}
	project, err := t.kit.Workspace.UpdateProject(ctx, projectID, args.Name, args.Description)
	if err != nil {
		return toolError(updateProjectToolName, err.Error())
	}
	return toolJSON(updateProjectToolName, project)
}

type setRepositoryProjectsArgs struct {
	RepositoryID string   `json:"repository_id"`
	ProjectIDs   []string `json:"project_ids"`
}

type setRepositoryProjectsTool struct {
	kit *ToolKit
}

func newSetRepositoryProjectsTool(kit *ToolKit) port.ToolExecutor {
	return &setRepositoryProjectsTool{kit: kit}
}

func (t *setRepositoryProjectsTool) Name() string { return setRepositoryProjectsToolName }

func (t *setRepositoryProjectsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: setRepositoryProjectsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"repository_id": map[string]interface{}{
						"type": "string",
					},
					"project_ids": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required": []string{"repository_id", "project_ids"},
			},
		},
	}
}

func (t *setRepositoryProjectsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	if t.kit.Workspace == nil {
		return toolError(setRepositoryProjectsToolName, "workspace is not available")
	}
	var args setRepositoryProjectsArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(setRepositoryProjectsToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	repositoryID, err := uuid.Parse(strings.TrimSpace(args.RepositoryID))
	if err != nil {
		return toolError(setRepositoryProjectsToolName, "invalid repository_id")
	}
	projectIDs := make([]uuid.UUID, 0, len(args.ProjectIDs))
	for _, raw := range args.ProjectIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return toolError(setRepositoryProjectsToolName, fmt.Sprintf("invalid project id: %s", raw))
		}
		projectIDs = append(projectIDs, parsed)
	}
	repo, err := t.kit.Workspace.SetRepositoryProjects(ctx, repositoryID, projectIDs)
	if err != nil {
		return toolError(setRepositoryProjectsToolName, err.Error())
	}
	return toolJSON(setRepositoryProjectsToolName, repo)
}
