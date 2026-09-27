package board

// get_deploy_logs is what is left of the old deploy-watch tool trio: reading
// what happened to a task's merge commit and rolling it back are now
// release_tools.go's job (get_release, watch_release, rollback_release), because
// both now key off a release row rather than a bare GitHub signal. The log
// reader stays here and stays task-scoped, resolving its task from the
// argument or the run context exactly like the release tools do.
//
// It defaults to the failing job of the task's RELEASE now, falling back to
// the legacy per-task deploy watch only when the task has no release (a build
// with no release service wired, or a task that never opened one) — see
// resolveDeployTask and the fallback in Execute below.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploywatch"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// deployLogsToolName is domain's, not this file's: the policy layer decides
// things about it by name in more than one place, and a literal that matched
// in only one of them would silently widen or narrow what a run may do.
const deployLogsToolName = domain.DeployLogsToolName

// DeployWatch is the legacy per-task deploy signal this toolkit falls back to
// when a task has no release (get_deploy_logs's job-id default). Nil on a
// build with no GitHub token store, which is why get_deploy_logs is
// registered on the same condition as the PR tools rather than on this being
// set.
type DeployWatch interface {
	Status(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.DeployWatchStatus, error)
	JobLogs(ctx context.Context, repositoryID uuid.UUID, jobID int64, maxChars int) (deploywatch.LogResult, error)
	EndpointLogs(ctx context.Context, repositoryID uuid.UUID, env string, maxChars int) (deploywatch.LogResult, error)
}

// --------------------------------------------------------------- get_deploy_logs

type deployLogsTool struct{ kit *ToolKit }

func newDeployLogsTool(kit *ToolKit) port.ToolExecutor { return &deployLogsTool{kit: kit} }

func (t *deployLogsTool) Name() string { return deployLogsToolName }

func (t *deployLogsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: deployLogsToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"source": map[string]interface{}{
						"type": "string",
						"enum": []string{deploywatch.LogSourceActionsJob, deploywatch.LogSourceEndpoint},
					},
					"job_id": map[string]interface{}{
						"type": "integer",
					},
					"env": map[string]interface{}{
						"type": "string",
						"enum": domain.DeployEnvs(),
					},
				},
			},
		},
	}
}

func (t *deployLogsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
		Source string `json:"source"`
		JobID  int64  `json:"job_id"`
		Env    string `json:"env"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deployLogsToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.DeployWatch == nil {
		return toolError(deployLogsToolName, "the deploy watch is not configured on this deployment")
	}
	taskID, repositoryID, res := t.kit.resolveDeployTask(ctx, deployLogsToolName, args.TaskID)
	if res != nil {
		return *res
	}

	source := strings.TrimSpace(args.Source)
	if source == "" {
		source = deploywatch.LogSourceActionsJob
	}
	if source == deploywatch.LogSourceEndpoint {
		env := strings.TrimSpace(args.Env)
		if env == "" {
			env = domain.DeployEnvProd
		}
		if !domain.ValidDeployEnv(env) {
			return toolError(deployLogsToolName, "env must be one of stage, preprod, prod")
		}
		result, err := t.kit.DeployWatch.EndpointLogs(ctx, repositoryID, env, 0)
		if err != nil {
			return toolError(deployLogsToolName, err.Error())
		}
		return toolJSON(deployLogsToolName, result)
	}

	jobID := args.JobID
	if jobID == 0 {
		// The release's own record of its failed job is the primary source now
		// (deploy_release/watch_release write it as the sweeper resolves the
		// deploy) — resolving from the task's deploy watch is only what is left
		// for a task with no release (no release service wired, or a component
		// whose mode never opened one).
		if t.kit.Releases != nil {
			if rel, relErr := t.kit.Releases.ForTask(ctx, repositoryID, taskID); relErr == nil {
				if rel.Deploy != nil && rel.Deploy.FailedJob != nil && rel.Deploy.FailedJob.ID != 0 {
					jobID = rel.Deploy.FailedJob.ID
				}
			}
		}
	}
	if jobID == 0 {
		status, err := t.kit.DeployWatch.Status(ctx, repositoryID, taskID)
		if err != nil {
			return toolError(deployLogsToolName, err.Error())
		}
		if status.FailedJob == nil || status.FailedJob.ID == 0 {
			return toolError(deployLogsToolName, deployLogsNoFailedJobKey.Render(stateInput{State: string(status.State)}))
		}
		jobID = status.FailedJob.ID
	}
	result, err := t.kit.DeployWatch.JobLogs(ctx, repositoryID, jobID, 0)
	if err != nil {
		return toolError(deployLogsToolName, err.Error())
	}
	return toolJSON(deployLogsToolName, result)
}

// resolveDeployTask is the shared argument handling of the deploy/release
// tools: resolve the task from the argument or the run context, then resolve
// its repository the same way every other task tool does — so a task_id from
// another repository is not-found rather than another repository's
// production.
func (kit *ToolKit) resolveDeployTask(ctx context.Context, tool, ref string) (uuid.UUID, uuid.UUID, *domain.ToolResult) {
	taskID, err := kit.resolveTaskArg(ctx, ref)
	if err != nil {
		res := toolError(tool, err.Error())
		return uuid.Nil, uuid.Nil, &res
	}
	repositoryID, err := kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		res := toolError(tool, err.Error())
		return uuid.Nil, uuid.Nil, &res
	}
	return taskID, repositoryID, nil
}
