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
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"task_id", "cases"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "string"},
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
		return in, errors.New(testCaseInvalidCriterionIDKey.Render(testCaseInvalidCriterionIDInput{Title: a.Title, RawID: a.CriterionID}))
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
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"test_case_id", "status"},
				"properties": map[string]interface{}{
					"test_case_id": map[string]interface{}{"type": "string"},
					"status":       map[string]interface{}{"type": "string", "enum": testCaseStatusEnum()},
					"actual":       map[string]interface{}{"type": "string"},
					"evidence":     map[string]interface{}{"type": "string"},
					"notes":        map[string]interface{}{"type": "string"},
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
			"title":    map[string]interface{}{"type": "string"},
			"category": map[string]interface{}{"type": "string", "enum": testCaseCategoryEnum()},
			"status":   map[string]interface{}{"type": "string", "enum": testCaseStatusEnum()},
			"expected": map[string]interface{}{"type": "string"},
			"actual":   map[string]interface{}{"type": "string"},
			"evidence": map[string]interface{}{"type": "string"},
			"notes":    map[string]interface{}{"type": "string"},
			"criterion_id": map[string]interface{}{
				"type": "string",
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
