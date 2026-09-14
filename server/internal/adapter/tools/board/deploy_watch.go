package board

// The three tools that make up the second half of the release loop: watch the
// deploy the merge produced, read the log when it fails, roll it back when it
// has to be rolled back.
//
// They live beside merge_pr.go and release.go because they are task-scoped —
// they resolve their task from the argument or from the run context exactly as
// those do, and they mean nothing without one. Everything they refuse is
// refused in application/deploywatch, because a gate that only holds for a
// caller whose model was in the mood to check is not a gate.
//
// The one thing done HERE and nowhere else is the park. A deploy takes minutes
// and no tool may spend them: when the watch is still pending, this tool hands
// back a domain.ResourceBlock instead of a result, the agent loop ends the
// turn, the runner parks the card on domain.ResourceDeployWatch, and
// application/board.DeploySweeper re-dispatches it once GitHub says the deploy
// settled. That is the same mechanism a held test phone uses, for the same
// reason: the thing being waited for is outside the agent's influence, and a
// model that keeps asking only burns the run's budget.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploywatch"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Names live in domain: the policy layer decides things about them by name in
// more than one place (role allow-lists, the verdict-column narrowing, the
// workspace uplift) and a literal that matched in only one of them would
// silently widen what a run may do.
const (
	deployStatusToolName    = domain.DeployStatusToolName
	deployLogsToolName      = domain.DeployLogsToolName
	rollbackReleaseToolName = domain.RollbackReleaseToolName
)

// DeployWatch is the deploy-watch use case this toolkit exposes. Nil on a build
// with no GitHub token store, which is why the three tools are registered
// conditionally rather than always.
type DeployWatch interface {
	Status(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.DeployWatchStatus, error)
	JobLogs(ctx context.Context, repositoryID uuid.UUID, jobID int64, maxChars int) (deploywatch.LogResult, error)
	EndpointLogs(ctx context.Context, repositoryID uuid.UUID, env string, maxChars int) (deploywatch.LogResult, error)
	Rollback(ctx context.Context, req deploywatch.RollbackRequest) (domain.TaskRollbackResult, error)
}

// -------------------------------------------------------- get_task_deploy_status

type deployStatusTool struct{ kit *ToolKit }

func newDeployStatusTool(kit *ToolKit) port.ToolExecutor { return &deployStatusTool{kit: kit} }

func (t *deployStatusTool) Name() string { return deployStatusToolName }

func (t *deployStatusTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: deployStatusToolName,
			Description: "Report what happened in production to the commit this task's pull request merge produced. " +
				"Call it after merge_task_pull_request (and after trigger_release, if the repository releases that way) to find out whether the change actually shipped. " +
				"It resolves one of four states: `success` (the deploy finished green — production is now running this task's code), " +
				"`failure` (the deploy finished red — read the log with get_deploy_logs and roll back with rollback_task_release), " +
				"`pending` (still running), or `no_signal` (nothing anywhere reports a deploy of this commit, which means this repository does not deploy on merge — that is an answer, not a problem to solve). " +
				"It works for both kinds of repository: one with a GitHub Actions deploy job, and one that deploys on push (Vercel and similar), where the signal is the commit status the provider writes. " +
				"When the deploy is still running the call does NOT return a status — it parks this task until the deploy settles and your run ends. That is correct and expected: do not try to poll, wait, or sleep. You (or the next run) will be woken with the answer. " +
				"Pass `wait: false` if you only want to look without parking.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"wait": map[string]interface{}{
						"type": "boolean",
						"description": "Default true: a deploy that is still running parks this task and ends the run, and the task is re-dispatched when it finishes. " +
							"Set false to get `pending` back as an ordinary result instead — use that only when you are reporting on the deploy rather than waiting for it.",
					},
				},
			},
		},
	}
}

func (t *deployStatusTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
		Wait   *bool  `json:"wait"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deployStatusToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.DeployWatch == nil {
		return toolError(deployStatusToolName, "the deploy watch is not configured on this deployment")
	}
	taskID, repositoryID, res := t.kit.resolveDeployTask(ctx, deployStatusToolName, args.TaskID)
	if res != nil {
		return *res
	}

	status, err := t.kit.DeployWatch.Status(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(deployStatusToolName, err.Error())
	}

	wait := args.Wait == nil || *args.Wait
	if wait && status.State == domain.DeployWatchPending {
		// Not an error and not a result: a park. The loop stops the turn here
		// and the runner moves the card; nothing in this process waits.
		return domain.ToolResult{
			Name: deployStatusToolName,
			Content: fmt.Sprintf("The deploy of %s is still running. This task is parked until it finishes and will be picked up again then — nothing further to do in this run.",
				domain.ShortSHA(status.MergeSHA)),
			ResourceBlock: &domain.ResourceBlock{
				Resource: domain.ResourceDeployWatch,
				Detail: fmt.Sprintf("waiting for the %s deploy of %s (%s)",
					status.Env, domain.ShortSHA(status.MergeSHA), status.Signal),
			},
		}
	}
	return toolJSON(deployStatusToolName, deployStatusPayload(status))
}

// deployStatusPayload adds the "so what" to the raw status. A model handed a
// state and nothing else invents the next step; naming it here means the same
// state always produces the same move.
func deployStatusPayload(status domain.DeployWatchStatus) map[string]any {
	payload := map[string]any{"status": status}
	switch status.State {
	case domain.DeployWatchSuccess:
		payload["next"] = "Production is running this task's code. Watch the health window: an incident opened before it closes belongs to THIS release. " +
			"Write NO comment for this: the release is already on the card and a \"deploy succeeded\" note is noise. Comment only if something about the deploy needs a person."
	case domain.DeployWatchFailure:
		payload["next"] = "The deploy failed. Read the log with get_deploy_logs (pass the failed job id below if there is one), post a summary of the failure on the task, " +
			"then call rollback_task_release. Do not try to fix the code — that is the developer's, through need_revision."
	case domain.DeployWatchNoSignal:
		payload["next"] = "Nothing deployed this commit automatically. That is either a repository that does not deploy on merge, or one whose CI could not run at all " +
			"(billing, spending limit, Actions disabled — the pipeline comment on this task says so when that is what happened). " +
			"Look for the repository's own deploy procedure (a deploy script, a Makefile target, the deploy steps in its README or docs) and, if there is one, run it with run_terminal and verify the environment answers afterwards. " +
			"If the repository has no local deploy path, move the task to `blocked` with the reason — do not leave it in done claiming a release that never happened."
	case domain.DeployWatchUnknown:
		payload["next"] = "The deploy could not be resolved. Report exactly that on the task; do not report the task as released."
	default:
		payload["next"] = "The deploy has not finished. Report it as still running."
	}
	return payload
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
			Description: "Read the log behind a deploy. Two sources: `actions_job` (default) fetches the failing GitHub Actions deploy job's log — pass the `job_id` get_task_deploy_status reported, or omit it and the failing job of this task's deploy is used; " +
				"`logs_url` fetches the environment's own log endpoint, if one is recorded on the deploy target. " +
				"The result is a SUMMARY, not a dump: the error-looking lines are lifted out first and the tail follows them, so paste the relevant part into your comment rather than the whole thing. " +
				"Use it on a failed deploy before rolling back, and on a successful one whose environment then went unhealthy.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"source": map[string]interface{}{
						"type":        "string",
						"enum":        []string{deploywatch.LogSourceActionsJob, deploywatch.LogSourceEndpoint},
						"description": "actions_job (the deploy job's CI log, default) or logs_url (the application's own log endpoint)",
					},
					"job_id": map[string]interface{}{
						"type":        "integer",
						"description": "Actions job id, from get_task_deploy_status's failed_job. Omit to use this task's failing deploy job.",
					},
					"env": map[string]interface{}{
						"type":        "string",
						"enum":        domain.DeployEnvs(),
						"description": "Which environment's logs_url to read. Defaults to prod. Only used with source=logs_url.",
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
		// Resolving the job from the task is what makes the common call —
		// "show me why the deploy failed" — a single tool call instead of two.
		status, err := t.kit.DeployWatch.Status(ctx, repositoryID, taskID)
		if err != nil {
			return toolError(deployLogsToolName, err.Error())
		}
		if status.FailedJob == nil || status.FailedJob.ID == 0 {
			return toolError(deployLogsToolName,
				"this task's deploy has no failing GitHub Actions job to read: state is "+string(status.State)+
					". If the repository deploys on push there is no CI log at all — try source=logs_url, or read the provider's own link in the status.")
		}
		jobID = status.FailedJob.ID
	}
	result, err := t.kit.DeployWatch.JobLogs(ctx, repositoryID, jobID, 0)
	if err != nil {
		return toolError(deployLogsToolName, err.Error())
	}
	return toolJSON(deployLogsToolName, result)
}

// ---------------------------------------------------------- rollback_task_release

type rollbackReleaseTool struct{ kit *ToolKit }

func newRollbackReleaseTool(kit *ToolKit) port.ToolExecutor { return &rollbackReleaseTool{kit: kit} }

func (t *rollbackReleaseTool) Name() string { return rollbackReleaseToolName }

func (t *rollbackReleaseTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: rollbackReleaseToolName,
			Description: "Roll production back off this task's release. Call it when the deploy of this task's merge commit FAILED, or when the environment went unhealthy inside the window after this task deployed — and not otherwise. " +
				"What it does depends on the repository: where a deploy workflow exists it re-deploys the last known-good commit; where the host deploys on push it reverts the merge commit on the default branch and pushes. You do not choose. " +
				"It refuses, changing nothing, when: the task never merged; the commit it merged is not what the environment is currently running (someone else has released since — rolling back would undo THEIR change); or nothing actually went wrong. " +
				"If the deploy target has auto_rollback OFF it executes nothing and returns `proposed: true` with the rollback written up for a human to confirm — report that on the task, do not look for another way to do it. " +
				"It ALWAYS returns `manual_steps`: things the mechanical rollback did not undo — a database migration, a feature flag, anything the task's own rollback_plan lists. " +
				"You must perform or explicitly report every one of them on the task. A rollback reported as complete when half of it was not is worse than one that says what it could not do.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"trigger"},
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"trigger": map[string]interface{}{
						"type":        "string",
						"enum":        []string{deploywatch.RollbackTriggerDeployFailed, deploywatch.RollbackTriggerHealthIncident},
						"description": "Why you are rolling back: deploy_failed (get_task_deploy_status returned failure) or health_incident (the environment went unhealthy after this release).",
					},
					"note": map[string]interface{}{
						"type":        "string",
						"description": "One sentence on what you actually observed — the failing step, the error, the health check that went red. It goes on the card and into the incident.",
					},
					"env": map[string]interface{}{
						"type":        "string",
						"enum":        domain.DeployEnvs(),
						"description": "Which environment to roll back. Defaults to prod.",
					},
				},
			},
		},
	}
}

func (t *rollbackReleaseTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID  string `json:"task_id"`
		Trigger string `json:"trigger"`
		Note    string `json:"note"`
		Env     string `json:"env"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(rollbackReleaseToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.DeployWatch == nil {
		return toolError(rollbackReleaseToolName, "rollback is not configured on this deployment")
	}
	taskID, repositoryID, res := t.kit.resolveDeployTask(ctx, rollbackReleaseToolName, args.TaskID)
	if res != nil {
		return *res
	}
	env := strings.TrimSpace(args.Env)
	if env == "" {
		env = domain.DeployEnvProd
	}
	if !domain.ValidDeployEnv(env) {
		return toolError(rollbackReleaseToolName, "env must be one of stage, preprod, prod")
	}

	result, err := t.kit.DeployWatch.Rollback(ctx, deploywatch.RollbackRequest{
		RepositoryID: repositoryID,
		TaskID:       taskID,
		Env:          env,
		Trigger:      strings.TrimSpace(args.Trigger),
		Note:         strings.TrimSpace(args.Note),
		AgentName:    t.kit.agentName(ctx),
	})
	if err != nil {
		if isRollbackRefusal(err) {
			// Same contract as the merge tool's refusals: an error, and an
			// explicit instruction not to retry. Every one of these is a state
			// only a board action or a human can change, and a model's default
			// answer to an error is another attempt.
			return toolError(rollbackReleaseToolName, err.Error()+
				"\n\nNothing was rolled back. Do not retry rollback_task_release — it will refuse again until the state above changes. Report this on the task instead.")
		}
		return toolError(rollbackReleaseToolName, err.Error())
	}
	return toolJSON(rollbackReleaseToolName, result)
}

// isRollbackRefusal reports whether the error is a gate saying no, as opposed to
// GitHub, git or the network failing. Only the first kind is pointless to retry.
func isRollbackRefusal(err error) bool {
	for _, sentinel := range []error{
		domain.ErrRollbackNotMerged,
		domain.ErrRollbackNotOwner,
		domain.ErrRollbackNoTrigger,
		domain.ErrRollbackNoMechanism,
		domain.ErrRollbackColumn,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// agentNameFromContext names the role acting, for the audit entry and the card.
//
// The run context carries the agent's id and not its name, so the name is read
// off the roster when one is wired and the id is used otherwise. Neither is
// allowed to fail the rollback: an unattributed rollback in the audit log is
// bad, a production outage left standing because a name lookup failed is worse.
func (kit *ToolKit) agentName(ctx context.Context) string {
	id := registry.AgentIDFromContext(ctx)
	if id == uuid.Nil {
		return ""
	}
	if kit.Team != nil {
		if agents, err := kit.Team.ListAgents(ctx); err == nil {
			for _, a := range agents {
				if a.ID == id {
					return a.Name
				}
			}
		}
	}
	return id.String()
}

// resolveDeployTask is the shared argument handling of the three tools: resolve
// the task from the argument or the run context, then resolve its repository
// the same way every other task tool does — so a task_id from another
// repository is not-found rather than another repository's production.
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
