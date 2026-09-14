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
			Name:        listCriteriaToolName,
			Description: "List the acceptance criteria of a board task with their completion state. Complete every criterion before moving a task to ready_for_qa or done.",
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
			Name:        setCriterionToolName,
			Description: "Mark one acceptance criterion completed (or not). Only mark a criterion completed after you have actually verified it.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"criterion_id", "completed"},
				"properties": map[string]interface{}{
					"criterion_id": map[string]interface{}{"type": "string", "description": "Criterion UUID (from list_acceptance_criteria)"},
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
		return toolError(setCriterionToolName, "invalid criterion_id")
	}
	item, err := t.kit.Tasks.SetTaskCriterionCompleted(ctx, criterionID, args.Completed)
	if err != nil {
		return toolError(setCriterionToolName, err.Error())
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
			Description: "Drop ONE acceptance criterion from this task's scope, with the reason. Use it only for a criterion that is deliberately " +
				"not being done — out of scope, superseded by another decision, impossible as written, moved to another task. " +
				"It is NOT a way past a criterion you simply have not implemented: if the work is missing, do the work and call set_criterion_completed. " +
				"The reason is stored on the criterion and posted as a task comment, so the decision is visible to the humans reading the card.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"criterion_id", "reason"},
				"properties": map[string]interface{}{
					"criterion_id": map[string]interface{}{"type": "string", "description": "Criterion UUID (from list_acceptance_criteria)"},
					"reason":       map[string]interface{}{"type": "string", "description": "Why this criterion is not being done. One or two sentences, concrete."},
					"canceled": map[string]interface{}{
						"type":        "boolean",
						"description": "Defaults to true. Pass false to put a cancelled criterion back in scope.",
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
		return toolError(cancelCriterionToolName, "invalid criterion_id")
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
		return toolError(cancelCriterionToolName, err.Error())
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
			Description: "Record YOUR verdict on one acceptance criterion after actually verifying it — QA while the task is in ready_for_qa/in_qa, PM while it is in pm_uat. " +
				"The implementer's checkmark is a claim, not proof: every criterion needs your own approved=true before the task can leave your review phase. " +
				"A rejection (approved=false) requires a note saying exactly what failed and how you observed it; that note is shown on the task and read by the developer in need_revision.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"criterion_id", "approved"},
				"properties": map[string]interface{}{
					"criterion_id": map[string]interface{}{"type": "string", "description": "Criterion UUID (from list_acceptance_criteria)"},
					"approved":     map[string]interface{}{"type": "boolean", "description": "true only after you verified the criterion yourself"},
					"note": map[string]interface{}{
						"type":        "string",
						"description": "Required when approved=false: what failed, expected vs actual, how to reproduce. Optional evidence summary when approving.",
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
		return toolError(reviewCriterionToolName, "invalid criterion_id")
	}
	agentID, err := resolveAgentID(ctx)
	if err != nil {
		return toolError(reviewCriterionToolName, err.Error())
	}
	check, err := t.kit.Tasks.ReviewTaskCriterion(ctx, criterionID, agentID, args.Approved, args.Note)
	if err != nil {
		return toolError(reviewCriterionToolName, err.Error())
	}
	return toolJSON(reviewCriterionToolName, map[string]any{"check": check})
}
