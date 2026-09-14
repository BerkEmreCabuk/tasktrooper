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
	listTestCasesToolName     = "list_test_cases"
	recordTestCasesToolName   = "record_test_cases"
	setTestCaseResultToolName = "set_test_case_result"
)

type listTestCasesTool struct {
	kit *ToolKit
}

func newListTestCasesTool(kit *ToolKit) port.ToolExecutor { return &listTestCasesTool{kit: kit} }

func (t *listTestCasesTool) Name() string { return listTestCasesToolName }

func (t *listTestCasesTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listTestCasesToolName,
			Description: "List the test cases recorded on a board task with their status, expectation and evidence. " +
				"This is the round the task was actually given — read it before re-testing, and before judging whether a change was verified.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"task_id"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "string", "description": "Board task UUID or its board key (e.g. \"T-1\")."},
				},
			},
		},
	}
}

func (t *listTestCasesTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(listTestCasesToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(listTestCasesToolName, err.Error())
	}
	items, err := t.kit.Tasks.ListTestCases(ctx, taskID)
	if err != nil {
		return toolError(listTestCasesToolName, err.Error())
	}
	return toolJSON(listTestCasesToolName, map[string]any{
		"test_cases": items,
		"summary":    domain.SummarizeTestCases(items),
	})
}

type recordTestCasesTool struct {
	kit *ToolKit
}

func newRecordTestCasesTool(kit *ToolKit) port.ToolExecutor { return &recordTestCasesTool{kit: kit} }

func (t *recordTestCasesTool) Name() string { return recordTestCasesToolName }

func (t *recordTestCasesTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: recordTestCasesToolName,
			Description: "Write the task's test cases onto the card — the whole matrix, not only the acceptance criteria. " +
				"Derive the cases from what was ASKED FOR and what the request IMPLIES: happy path, boundaries, invalid input, auth, empty state, " +
				"async/worker side effects, visual states, and regression of adjacent behaviour. Record them BEFORE executing (status=planned), " +
				"then call this again (or set_test_case_result) with each verdict. Cases are matched by title, so re-sending a title updates that case. " +
				"Record the cases you considered and rejected too, as status=invalid with the reason in notes — a case that was thought about and " +
				"dismissed is part of the evidence, not noise.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"task_id", "cases"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "string", "description": "Board task UUID or its board key (e.g. \"T-1\")."},
					"cases": map[string]interface{}{
						"type":  "array",
						"items": testCaseItemSchema(),
					},
				},
			},
		},
	}
}

// testCaseArg is the wire shape, and criterion_id is a plain string in it on
// purpose: a model asked for an "optional" uuid sends `""` about as often as it
// omits the field, and decoding that straight into *uuid.UUID fails the whole
// call with "invalid UUID length: 0" — a batch of twenty cases lost to an empty
// string in one of them.
type testCaseArg struct {
	CriterionID string                  `json:"criterion_id"`
	Title       string                  `json:"title"`
	Category    domain.TestCaseCategory `json:"category"`
	Status      domain.TestCaseStatus   `json:"status"`
	Expected    string                  `json:"expected"`
	Actual      string                  `json:"actual"`
	Evidence    string                  `json:"evidence"`
	Notes       string                  `json:"notes"`
	Position    int                     `json:"position"`
}

func (a testCaseArg) toInput() (domain.TaskTestCaseInput, error) {
	in := domain.TaskTestCaseInput{
		Title:    a.Title,
		Category: a.Category,
		Status:   a.Status,
		Expected: a.Expected,
		Actual:   a.Actual,
		Evidence: a.Evidence,
		Notes:    a.Notes,
		Position: a.Position,
	}
	if strings.TrimSpace(a.CriterionID) == "" {
		return in, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(a.CriterionID))
	if err != nil {
		return in, fmt.Errorf("case %q has an invalid criterion_id %q; use an id from list_acceptance_criteria, or leave it empty", a.Title, a.CriterionID)
	}
	in.CriterionID = &id
	return in, nil
}

func (t *recordTestCasesTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string        `json:"task_id"`
		Cases  []testCaseArg `json:"cases"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(recordTestCasesToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if len(args.Cases) == 0 {
		return toolError(recordTestCasesToolName, "cases is empty: send the derived test cases")
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(recordTestCasesToolName, err.Error())
	}
	inputs := make([]domain.TaskTestCaseInput, 0, len(args.Cases))
	for _, c := range args.Cases {
		in, cErr := c.toInput()
		if cErr != nil {
			return toolError(recordTestCasesToolName, cErr.Error())
		}
		inputs = append(inputs, in)
	}
	items, err := t.kit.Tasks.RecordTestCases(ctx, taskID, inputs)
	if err != nil {
		return toolError(recordTestCasesToolName, err.Error())
	}
	return toolJSON(recordTestCasesToolName, map[string]any{
		"test_cases": items,
		"summary":    domain.SummarizeTestCases(items),
	})
}

type setTestCaseResultTool struct {
	kit *ToolKit
}

func newSetTestCaseResultTool(kit *ToolKit) port.ToolExecutor {
	return &setTestCaseResultTool{kit: kit}
}

func (t *setTestCaseResultTool) Name() string { return setTestCaseResultToolName }

func (t *setTestCaseResultTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: setTestCaseResultToolName,
			Description: "Record the verdict on ONE test case you just executed: passed, failed (with what you actually observed), " +
				"skipped (with what blocked it) or invalid (with why it is not a valid case). Fields you leave empty keep their stored value.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"test_case_id", "status"},
				"properties": map[string]interface{}{
					"test_case_id": map[string]interface{}{"type": "string", "description": "Test case UUID (from list_test_cases or record_test_cases)"},
					"status":       map[string]interface{}{"type": "string", "enum": testCaseStatusEnum(), "description": "passed | failed | skipped | invalid"},
					"actual":       map[string]interface{}{"type": "string", "description": "Required for failed: what you observed instead of the expectation."},
					"evidence":     map[string]interface{}{"type": "string", "description": "The command and its output, the request/response, or the screenshot path that proves this verdict."},
					"notes":        map[string]interface{}{"type": "string", "description": "Required for skipped (what blocked it) and invalid (why it is not a valid case)."},
				},
			},
		},
	}
}

func (t *setTestCaseResultTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TestCaseID string `json:"test_case_id"`
		Status     string `json:"status"`
		Actual     string `json:"actual"`
		Evidence   string `json:"evidence"`
		Notes      string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(setTestCaseResultToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	id, err := uuid.Parse(args.TestCaseID)
	if err != nil {
		return toolError(setTestCaseResultToolName, "invalid test_case_id")
	}
	item, err := t.kit.Tasks.SetTestCaseResult(ctx, id, domain.TaskTestCaseInput{
		Status:   domain.TestCaseStatus(args.Status),
		Actual:   args.Actual,
		Evidence: args.Evidence,
		Notes:    args.Notes,
	})
	if err != nil {
		return toolError(setTestCaseResultToolName, err.Error())
	}
	return toolJSON(setTestCaseResultToolName, map[string]any{"test_case": item})
}

func testCaseItemSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"title"},
		"properties": map[string]interface{}{
			"title":    map[string]interface{}{"type": "string", "description": "What the case does, in one line. This is the case's identity — re-sending the same title updates it."},
			"category": map[string]interface{}{"type": "string", "enum": testCaseCategoryEnum(), "description": "Which dimension this case covers."},
			"status":   map[string]interface{}{"type": "string", "enum": testCaseStatusEnum(), "description": "planned before you run it; then passed | failed | skipped | invalid."},
			"expected": map[string]interface{}{"type": "string", "description": "The observable result the request implies."},
			"actual":   map[string]interface{}{"type": "string", "description": "Required for failed: what actually happened."},
			"evidence": map[string]interface{}{"type": "string", "description": "Command + output, request/response, or screenshot path."},
			"notes":    map[string]interface{}{"type": "string", "description": "Required for skipped (what blocked it) and invalid (why it is not a valid case)."},
			"criterion_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: the acceptance criterion this case exercises. Leave empty for a case no criterion states — those are the ones worth writing down.",
			},
		},
	}
}

func testCaseStatusEnum() []string {
	out := make([]string, 0, len(domain.TestCaseStatuses))
	for _, s := range domain.TestCaseStatuses {
		out = append(out, string(s))
	}
	return out
}

func testCaseCategoryEnum() []string {
	out := make([]string, 0, len(domain.TestCaseCategories))
	for _, c := range domain.TestCaseCategories {
		out = append(out, string(c))
	}
	return out
}
