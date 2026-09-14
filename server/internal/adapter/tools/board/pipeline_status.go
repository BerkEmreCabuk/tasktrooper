package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const getPipelineStatusToolName = "get_pipeline_status"

// pipelineStatusJobOutput mirrors domain.TaskPipelineJob but only exposes
// Output for failed jobs (tail-truncated), keeping the payload small for
// jobs that passed or were skipped.
type pipelineStatusJobOutput struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Output   string `json:"output,omitempty"`
}

type getPipelineStatusTool struct {
	kit *ToolKit
}

func newGetPipelineStatusTool(kit *ToolKit) port.ToolExecutor {
	return &getPipelineStatusTool{kit: kit}
}

func (t *getPipelineStatusTool) Name() string { return getPipelineStatusToolName }

func (t *getPipelineStatusTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        getPipelineStatusToolName,
			Description: "Get the most recent QA-gate pipeline run for a board task: overall status, trigger, and per-job results (failed jobs include tail-truncated output). Use this to see why a task landed back in need_revision.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"task_id"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "string", "description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis)."},
				},
			},
		},
	}
}

func (t *getPipelineStatusTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(getPipelineStatusToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(getPipelineStatusToolName, err.Error())
	}

	// Resolve the repository the same way sibling task-scoped tools do: the
	// registry context pins the agent's repository when set, so a task_id
	// from another repository fails the service's task-in-repo check instead
	// of leaking that repository's build logs.
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(getPipelineStatusToolName, err.Error())
	}
	pipeline, err := t.kit.Tasks.LatestTaskPipeline(ctx, repositoryID, taskID)
	if err != nil {
		if errors.Is(err, domain.ErrPipelineNotFound) {
			return toolJSON(getPipelineStatusToolName, map[string]any{"message": "no pipeline for this task"})
		}
		return toolError(getPipelineStatusToolName, err.Error())
	}

	jobs := make([]pipelineStatusJobOutput, 0, len(pipeline.Jobs))
	for _, j := range pipeline.Jobs {
		out := pipelineStatusJobOutput{
			Name:     j.Name,
			Status:   string(j.Status),
			ExitCode: j.ExitCode,
		}
		if j.Status == domain.PipelineJobStatusFailed {
			output := j.Output
			if len(output) > 4000 {
				output = output[len(output)-4000:]
			}
			out.Output = output
		}
		jobs = append(jobs, out)
	}

	out := map[string]any{
		"status":     string(pipeline.Status),
		"trigger":    string(pipeline.Trigger),
		"created_at": pipeline.CreatedAt.Format(time.RFC3339),
		"note":       pipeline.Note,
		"jobs":       jobs,
	}
	// "skipped" is a status an agent has never seen before; spell out what it
	// means so it does not read it as a failure to retry or a green build to
	// trust.
	if pipeline.Status == domain.PipelineStatusSkipped {
		out["hint"] = "No CI checks are configured for this repository, so nothing was built or tested. The task was allowed through the gate, but this run is NOT evidence that the code compiles or passes tests — verify the work yourself."
	}
	return toolJSON(getPipelineStatusToolName, out)
}
