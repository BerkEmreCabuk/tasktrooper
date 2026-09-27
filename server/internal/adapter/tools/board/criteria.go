package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	listCriteriaToolName    = "list_acceptance_criteria"
	setCriterionToolName    = "set_criterion_completed"
	reviewCriterionToolName = "review_criterion"
	cancelCriterionToolName = "cancel_criterion"
)

// criteriaInputs turns the plain strings an agent passes to create/update into
// positioned criterion inputs. Blank entries are dropped so a trailing empty
// line in a generated list does not become an unverifiable criterion, and so
// are board actions (see criteria_board_action.go) — those are returned to the
// caller so the tool result can say what was removed and why.
func criteriaInputs(texts []string) ([]domain.AcceptanceCriterionInput, []droppedCriterion) {
	items := make([]domain.AcceptanceCriterionInput, 0, len(texts))
	var dropped []droppedCriterion
	for _, text := range texts {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		if reason := boardActionReason(trimmed); reason != "" {
			dropped = append(dropped, droppedCriterion{Text: trimmed, Reason: reason})
			continue
		}
		items = append(items, domain.AcceptanceCriterionInput{Text: trimmed, Position: len(items) + 1})
	}
	return items, dropped
}

// invalidCriterionIDResult reports a criterion_id that failed uuid.Parse,
// echoing the value the agent actually sent — the raw "invalid criterion_id"
// gave it nothing to compare against its own tool call.
func invalidCriterionIDResult(toolName, rawID string) domain.ToolResult {
	return toolError(toolName, invalidCriterionIDKey.Render(rawIDInput{RawID: rawID}))
}

// criterionErrorResult turns a criteria-store error into tool output. A
// domain.ErrCriterionNotFound means the id it was given no longer resolves —
// almost always because acceptance_criteria were replaced since the agent
// last listed them — so the message says that and names the fix instead of
// surfacing the driver's bare "no rows in result set".
func criterionErrorResult(toolName, rawID string, err error) domain.ToolResult {
	if errors.Is(err, domain.ErrCriterionNotFound) {
		return toolError(toolName, criterionNotFoundStaleKey.Render(rawIDInput{RawID: rawID}))
	}
	return toolError(toolName, err.Error())
}

type listCriteriaTool struct {
	kit *ToolKit
}

func newListCriteriaTool(kit *ToolKit) port.ToolExecutor {
	return &listCriteriaTool{kit: kit}
}

func (t *listCriteriaTool) Name() string { return listCriteriaToolName }

func (t *listCriteriaTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listCriteriaToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"task_id"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

func (t *listCriteriaTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(listCriteriaToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(listCriteriaToolName, err.Error())
	}
	items, err := t.kit.Tasks.ListTaskCriteria(ctx, taskID)
	if err != nil {
		return toolError(listCriteriaToolName, err.Error())
	}
	return toolJSON(listCriteriaToolName, map[string]any{"criteria": items})
}

type setCriterionTool struct {
	kit *ToolKit
}

func newSetCriterionTool(kit *ToolKit) port.ToolExecutor {
	return &setCriterionTool{kit: kit}
}

func (t *setCriterionTool) Name() string { return setCriterionToolName }

func (t *setCriterionTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: setCriterionToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"criterion_id", "completed"},
				"properties": map[string]interface{}{
					"criterion_id": map[string]interface{}{"type": "string"},
					"completed":    map[string]interface{}{"type": "boolean"},
				},
			},
		},
	}
}

func (t *setCriterionTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		CriterionID string `json:"criterion_id"`
		Completed   bool   `json:"completed"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(setCriterionToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	criterionID, err := uuid.Parse(args.CriterionID)
	if err != nil {
		return invalidCriterionIDResult(setCriterionToolName, args.CriterionID)
	}
	item, err := t.kit.Tasks.SetTaskCriterionCompleted(ctx, criterionID, args.Completed)
	if err != nil {
		return criterionErrorResult(setCriterionToolName, args.CriterionID, err)
	}
	return toolJSON(setCriterionToolName, map[string]any{"criterion": item})
}

type cancelCriterionTool struct {
	kit *ToolKit
}

func newCancelCriterionTool(kit *ToolKit) port.ToolExecutor {
	return &cancelCriterionTool{kit: kit}
}

func (t *cancelCriterionTool) Name() string { return cancelCriterionToolName }

func (t *cancelCriterionTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: cancelCriterionToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"criterion_id", "reason"},
				"properties": map[string]interface{}{
					"criterion_id": map[string]interface{}{"type": "string"},
					"reason":       map[string]interface{}{"type": "string"},
					"canceled": map[string]interface{}{
						"type": "boolean",
					},
				},
			},
		},
	}
}

func (t *cancelCriterionTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		CriterionID string `json:"criterion_id"`
		Reason      string `json:"reason"`
		Canceled    *bool  `json:"canceled"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(cancelCriterionToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	criterionID, err := uuid.Parse(args.CriterionID)
	if err != nil {
		return invalidCriterionIDResult(cancelCriterionToolName, args.CriterionID)
	}
	canceled := true
	if args.Canceled != nil {
		canceled = *args.Canceled
	}
	// The author is best-effort: a run without an agent id on its context still
	// gets to cancel, it just signs the comment as the system.
	authorID := ""
	if agentID, aErr := resolveAgentID(ctx); aErr == nil {
		authorID = agentID.String()
	}
	item, err := t.kit.Tasks.SetTaskCriterionCanceled(ctx, criterionID, canceled, args.Reason, "agent", authorID)
	if err != nil {
		return criterionErrorResult(cancelCriterionToolName, args.CriterionID, err)
	}
	return toolJSON(cancelCriterionToolName, map[string]any{"criterion": item})
}

type reviewCriterionTool struct {
	kit *ToolKit
}

func newReviewCriterionTool(kit *ToolKit) port.ToolExecutor {
	return &reviewCriterionTool{kit: kit}
}

func (t *reviewCriterionTool) Name() string { return reviewCriterionToolName }

func (t *reviewCriterionTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: reviewCriterionToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"criterion_id", "approved"},
				"properties": map[string]interface{}{
					"criterion_id": map[string]interface{}{"type": "string"},
					"approved":     map[string]interface{}{"type": "boolean"},
					"note": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
}

func (t *reviewCriterionTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		CriterionID string `json:"criterion_id"`
		Approved    bool   `json:"approved"`
		Note        string `json:"note"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(reviewCriterionToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	criterionID, err := uuid.Parse(args.CriterionID)
	if err != nil {
		return invalidCriterionIDResult(reviewCriterionToolName, args.CriterionID)
	}
	agentID, err := resolveAgentID(ctx)
	if err != nil {
		return toolError(reviewCriterionToolName, err.Error())
	}
	check, err := t.kit.Tasks.ReviewTaskCriterion(ctx, criterionID, agentID, args.Approved, args.Note)
	if err != nil {
		return criterionErrorResult(reviewCriterionToolName, args.CriterionID, err)
	}
	return toolJSON(reviewCriterionToolName, map[string]any{"check": check})
}
