package board

// The release engineer's tool surface: read the release covering a task, move
// it through deploy/watch/verify, and close it with a verdict. Every tool
// resolves its task exactly like the deploy/merge tools do (the argument, or
// the run context) and then asks kit.Releases for the newest release carrying
// it — a release is never addressed by its own id from the outside, only
// discovered from the task on the card.
//
// watch_release is the one thing that behaves like a wait: while the release
// is still Watched() (deploying/verifying/rolling_back) the release sweeper is
// what moves it, not this run, so the tool hands back a domain.ResourceBlock
// instead of a result. That parks the card on domain.ResourceReleaseWatch, the
// agent loop ends the turn there, and the sweeper re-dispatches the run when a
// verdict is needed or something failed — the exact mechanism
// get_task_deploy_status used for domain.ResourceDeployWatch, kept identical
// on purpose: a model that keeps asking only burns the run's budget on a wait
// it cannot shorten.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	getReleaseToolName      = domain.GetReleaseToolName
	deployReleaseToolName   = domain.DeployReleaseToolName
	watchReleaseToolName    = domain.WatchReleaseToolName
	runSmokeChecksToolName  = domain.RunSmokeChecksToolName
	finishReleaseToolName   = domain.FinishReleaseToolName
	rollbackReleaseToolName = domain.ReleaseRollbackToolName
)

// releaseNotConfigured is the shared refusal every release tool gives when the
// service has not been wired — read at CALL time, like DeployWatch, because it
// is built later in the boot sequence than tool registration runs.
func releaseNotConfigured(tool string) domain.ToolResult {
	return toolError(tool, "the release service is not configured on this deployment")
}

// resolveRelease turns the tool's optional task_id argument into the release
// currently covering that task. ErrReleaseNotFound is given its own sentence:
// a model that only sees "release not found" tries the call again instead of
// reading why there might not be one.
func (kit *ToolKit) resolveRelease(ctx context.Context, tool, taskRef string) (domain.Release, *domain.ToolResult) {
	taskID, repositoryID, res := kit.resolveDeployTask(ctx, tool, taskRef)
	if res != nil {
		return domain.Release{}, res
	}
	rel, err := kit.Releases.ForAgent(ctx, repositoryID, taskID)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, domain.ErrReleaseNotFound) {
			msg += " — nothing has opened a release for this task: it may not have merged yet, its component's delivery mode may be none/batch (nothing to watch), or its delivery profile is not confirmed. get_task_pull_request or the card's release field says which."
		}
		out := toolError(tool, msg)
		return domain.Release{}, &out
	}
	return rel, nil
}

// releaseNextStep is the "so what" for a release's current status. Naming it
// once, here, means the same status always produces the same instruction
// across get_release and watch_release rather than each tool inventing its own.
func releaseNextStep(r domain.Release) string {
	switch r.Status {
	case domain.ReleaseDraft:
		return "Nothing to do — a human cuts this release when they are ready."
	case domain.ReleaseAwaitingVerdict:
		if r.Mode == domain.DeliveryBatch {
			return "Read get_release for the build/publish evidence (workflow run, local_run, or store_builds) and any smoke checks, then call finish_release or rollback_release. Read query_runtime_logs and list_runtime_errors too when the component has a bound runtime environment; when it does not, say so explicitly in the finish note instead of treating the gap as a pass."
		}
		return "Read query_runtime_logs and list_runtime_errors since deployed_at, then call finish_release or rollback_release."
	case domain.ReleaseFailed:
		if r.Mode == domain.DeliveryBatch {
			return "Read get_release: for a local run, its local_run.tail (and local_run.log_path); for github_actions, call get_deploy_logs. Then call rollback_release if the bad code is live or sitting on the default branch — or, if nothing can be done from here, report that on the task."
		}
		return "Read get_deploy_logs if there is a failed job, then call rollback_release — or, if nothing can be done from here, report that on the task."
	case domain.ReleasePending:
		if r.Mode == domain.DeliveryBatch {
			return "A human has cut this release — call deploy_release."
		}
		return "Call deploy_release."
	case domain.ReleaseDeploying, domain.ReleaseVerifying, domain.ReleaseRollingBack:
		return "Call watch_release."
	case domain.ReleaseReleased, domain.ReleaseRolledBack, domain.ReleaseSuperseded:
		return "Nothing to do."
	default:
		return "No instruction for status " + string(r.Status) + " — report it as-is."
	}
}

// ------------------------------------------------------------------ get_release

type getReleaseTool struct{ kit *ToolKit }

func newGetReleaseTool(kit *ToolKit) port.ToolExecutor { return &getReleaseTool{kit: kit} }

func (t *getReleaseTool) Name() string { return getReleaseToolName }

func (t *getReleaseTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getReleaseToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *getReleaseTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(getReleaseToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Releases == nil {
		return releaseNotConfigured(getReleaseToolName)
	}
	rel, res := t.kit.resolveRelease(ctx, getReleaseToolName, args.TaskID)
	if res != nil {
		return *res
	}
	return toolJSON(getReleaseToolName, map[string]any{"release": rel, "next": releaseNextStep(rel)})
}

// --------------------------------------------------------------- deploy_release

type deployReleaseTool struct{ kit *ToolKit }

func newDeployReleaseTool(kit *ToolKit) port.ToolExecutor { return &deployReleaseTool{kit: kit} }

func (t *deployReleaseTool) Name() string { return deployReleaseToolName }

func (t *deployReleaseTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: deployReleaseToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *deployReleaseTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deployReleaseToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Releases == nil {
		return releaseNotConfigured(deployReleaseToolName)
	}
	rel, res := t.kit.resolveRelease(ctx, deployReleaseToolName, args.TaskID)
	if res != nil {
		return *res
	}
	updated, err := t.kit.Releases.Deploy(ctx, rel.ID, domain.ReleaseActorAgent)
	if err != nil {
		if errors.Is(err, domain.ErrReleaseWrongStatus) || errors.Is(err, domain.ErrReleaseNoDeploy) {
			return toolError(deployReleaseToolName, err.Error()+
				"\n\nNothing was deployed. Do not retry deploy_release — it will refuse again until the release's status or delivery mode changes.")
		}
		return toolError(deployReleaseToolName, err.Error())
	}
	return toolJSON(deployReleaseToolName, map[string]any{"release": updated, "next": "call watch_release"})
}

// ---------------------------------------------------------------- watch_release

type watchReleaseTool struct{ kit *ToolKit }

func newWatchReleaseTool(kit *ToolKit) port.ToolExecutor { return &watchReleaseTool{kit: kit} }

func (t *watchReleaseTool) Name() string { return watchReleaseToolName }

func (t *watchReleaseTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: watchReleaseToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *watchReleaseTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(watchReleaseToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Releases == nil {
		return releaseNotConfigured(watchReleaseToolName)
	}
	rel, res := t.kit.resolveRelease(ctx, watchReleaseToolName, args.TaskID)
	if res != nil {
		return *res
	}
	updated, block, err := t.kit.Releases.Watch(ctx, rel.ID)
	if err != nil {
		return toolError(watchReleaseToolName, err.Error())
	}
	if block != nil {
		// Not an error and not a result: a park. The loop stops the turn here
		// and the runner moves the card; nothing in this process waits. Same
		// shape get_task_deploy_status used for domain.ResourceDeployWatch.
		return domain.ToolResult{
			Name: watchReleaseToolName,
			Content: fmt.Sprintf("Release %s (%s/%s) is still %s. This task is parked until it settles and will be picked up again then — nothing further to do in this run.",
				updated.Version, updated.Mode, updated.Executor, updated.Status),
			ResourceBlock: block,
		}
	}
	return toolJSON(watchReleaseToolName, map[string]any{"release": updated, "next": releaseNextStep(updated)})
}

// ------------------------------------------------------------ run_smoke_checks

type runSmokeChecksTool struct{ kit *ToolKit }

func newRunSmokeChecksTool(kit *ToolKit) port.ToolExecutor { return &runSmokeChecksTool{kit: kit} }

func (t *runSmokeChecksTool) Name() string { return runSmokeChecksToolName }

func (t *runSmokeChecksTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: runSmokeChecksToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

func (t *runSmokeChecksTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(runSmokeChecksToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Releases == nil {
		return releaseNotConfigured(runSmokeChecksToolName)
	}
	rel, res := t.kit.resolveRelease(ctx, runSmokeChecksToolName, args.TaskID)
	if res != nil {
		return *res
	}
	results, err := t.kit.Releases.RunSmoke(ctx, rel.ID)
	if err != nil {
		return toolError(runSmokeChecksToolName, err.Error())
	}
	passed := 0
	for _, r := range results {
		if r.OK {
			passed++
		}
	}
	summary := fmt.Sprintf("%d/%d smoke checks passed", passed, len(results))
	return toolJSON(runSmokeChecksToolName, map[string]any{"results": results, "summary": summary})
}

// --------------------------------------------------------------- finish_release

type finishReleaseTool struct{ kit *ToolKit }

func newFinishReleaseTool(kit *ToolKit) port.ToolExecutor { return &finishReleaseTool{kit: kit} }

func (t *finishReleaseTool) Name() string { return finishReleaseToolName }

func (t *finishReleaseTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: finishReleaseToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"note"},
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"note": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

func (t *finishReleaseTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(finishReleaseToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Releases == nil {
		return releaseNotConfigured(finishReleaseToolName)
	}
	note := strings.TrimSpace(args.Note)
	if note == "" {
		return toolError(finishReleaseToolName, "note is required: say what you checked (log window, error groups, smoke results) before confirming the release")
	}
	rel, res := t.kit.resolveRelease(ctx, finishReleaseToolName, args.TaskID)
	if res != nil {
		return *res
	}
	updated, err := t.kit.Releases.Finish(ctx, rel.ID, domain.ReleaseActorAgent, note)
	if err != nil {
		if errors.Is(err, domain.ErrReleaseWrongStatus) {
			return toolError(finishReleaseToolName, err.Error()+
				"\n\nNothing was finished. Do not retry finish_release as an agent — it refuses again until the release reaches awaiting_verdict, or a human overrides a failed one.")
		}
		return toolError(finishReleaseToolName, err.Error())
	}
	return toolJSON(finishReleaseToolName, map[string]any{"release": updated, "next": "Nothing to do — the task(s) are released."})
}

// ------------------------------------------------------------- rollback_release

type rollbackReleaseTool struct{ kit *ToolKit }

func newRollbackReleaseTool(kit *ToolKit) port.ToolExecutor { return &rollbackReleaseTool{kit: kit} }

func (t *rollbackReleaseTool) Name() string { return rollbackReleaseToolName }

func (t *rollbackReleaseTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: rollbackReleaseToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"reason", "note"},
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
					"reason": map[string]interface{}{
						"type": "string",
						"enum": []string{string(domain.RollbackDeployFailed), string(domain.RollbackVerifyFailed), string(domain.RollbackHealthIncident)},
					},
					"note": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

func (t *rollbackReleaseTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(rollbackReleaseToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Releases == nil {
		return releaseNotConfigured(rollbackReleaseToolName)
	}
	reason := domain.RollbackReason(strings.TrimSpace(args.Reason))
	switch reason {
	case domain.RollbackDeployFailed, domain.RollbackVerifyFailed, domain.RollbackHealthIncident:
	default:
		return toolError(rollbackReleaseToolName, "reason must be one of deploy_failed, verify_failed, health_incident")
	}
	note := strings.TrimSpace(args.Note)
	if note == "" {
		return toolError(rollbackReleaseToolName, "note is required: say what you actually observed")
	}
	rel, res := t.kit.resolveRelease(ctx, rollbackReleaseToolName, args.TaskID)
	if res != nil {
		return *res
	}
	updated, err := t.kit.Releases.Rollback(ctx, rel.ID, domain.ReleaseActorAgent, reason, note)
	if err != nil {
		if errors.Is(err, domain.ErrRollbackNeedsHuman) {
			// A successful call that executed nothing, not an error: an error
			// reads to a model as a broken system to route around, and this is
			// the expected outcome for an auto_rollback-off component.
			return toolJSON(rollbackReleaseToolName, map[string]any{
				"proposed": true,
				"message":  err.Error(),
			})
		}
		if errors.Is(err, domain.ErrReleaseWrongStatus) {
			return toolError(rollbackReleaseToolName, err.Error()+
				"\n\nNothing was rolled back. Do not retry rollback_release — it will refuse again until the release's status changes.")
		}
		return toolError(rollbackReleaseToolName, err.Error())
	}
	return toolJSON(rollbackReleaseToolName, map[string]any{
		"release": updated,
		"next":    "Perform or report EVERY manual step, then call watch_release.",
	})
}
