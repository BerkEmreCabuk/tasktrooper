package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestSetCriterionTool_NotFoundIsActionable(t *testing.T) {
	fake := &fakeTaskManager{criterionErr: domain.ErrCriterionNotFound}
	tool := newSetCriterionTool(&ToolKit{Tasks: fake})

	staleID := uuid.New().String()
	result := tool.Execute(context.Background(), `{"criterion_id":"`+staleID+`","completed":true}`)
	if !result.IsError {
		t.Fatalf("expected a tool error for a stale criterion id")
	}
	if !strings.Contains(result.Content, staleID) {
		t.Errorf("expected the error to name the criterion id %q, got: %s", staleID, result.Content)
	}
	if !strings.Contains(result.Content, "list_acceptance_criteria") {
		t.Errorf("expected the error to point at list_acceptance_criteria, got: %s", result.Content)
	}
	if strings.Contains(result.Content, "no rows in result set") {
		t.Errorf("raw driver error must not leak through: %s", result.Content)
	}
}

func TestSetCriterionTool_InvalidIDEchoesValue(t *testing.T) {
	fake := &fakeTaskManager{}
	tool := newSetCriterionTool(&ToolKit{Tasks: fake})

	result := tool.Execute(context.Background(), `{"criterion_id":"not-a-uuid","completed":true}`)
	if !result.IsError {
		t.Fatalf("expected a tool error for an invalid criterion id")
	}
	if !strings.Contains(result.Content, "not-a-uuid") {
		t.Errorf("expected the error to echo the invalid value, got: %s", result.Content)
	}
}

func TestCancelCriterionTool_NotFoundIsActionable(t *testing.T) {
	fake := &fakeTaskManager{criterionErr: domain.ErrCriterionNotFound}
	tool := newCancelCriterionTool(&ToolKit{Tasks: fake})

	staleID := uuid.New().String()
	result := tool.Execute(context.Background(), `{"criterion_id":"`+staleID+`","reason":"out of scope"}`)
	if !result.IsError {
		t.Fatalf("expected a tool error for a stale criterion id")
	}
	if !strings.Contains(result.Content, staleID) || !strings.Contains(result.Content, "list_acceptance_criteria") {
		t.Errorf("expected an actionable error naming the id and list_acceptance_criteria, got: %s", result.Content)
	}
}

func TestCancelCriterionTool_InvalidIDEchoesValue(t *testing.T) {
	fake := &fakeTaskManager{}
	tool := newCancelCriterionTool(&ToolKit{Tasks: fake})

	result := tool.Execute(context.Background(), `{"criterion_id":"not-a-uuid","reason":"out of scope"}`)
	if !result.IsError {
		t.Fatalf("expected a tool error for an invalid criterion id")
	}
	if !strings.Contains(result.Content, "not-a-uuid") {
		t.Errorf("expected the error to echo the invalid value, got: %s", result.Content)
	}
}

func TestReviewCriterionTool_NotFoundIsActionable(t *testing.T) {
	fake := &fakeTaskManager{criterionErr: domain.ErrCriterionNotFound}
	tool := newReviewCriterionTool(&ToolKit{Tasks: fake})
	ctx := registry.ContextWithAgentID(context.Background(), uuid.New())

	staleID := uuid.New().String()
	result := tool.Execute(ctx, `{"criterion_id":"`+staleID+`","approved":true}`)
	if !result.IsError {
		t.Fatalf("expected a tool error for a stale criterion id")
	}
	if !strings.Contains(result.Content, staleID) || !strings.Contains(result.Content, "list_acceptance_criteria") {
		t.Errorf("expected an actionable error naming the id and list_acceptance_criteria, got: %s", result.Content)
	}
	if strings.Contains(result.Content, "no rows in result set") {
		t.Errorf("raw driver error must not leak through: %s", result.Content)
	}
}

func TestReviewCriterionTool_InvalidIDEchoesValue(t *testing.T) {
	fake := &fakeTaskManager{}
	tool := newReviewCriterionTool(&ToolKit{Tasks: fake})
	ctx := registry.ContextWithAgentID(context.Background(), uuid.New())

	result := tool.Execute(ctx, `{"criterion_id":"not-a-uuid","approved":true}`)
	if !result.IsError {
		t.Fatalf("expected a tool error for an invalid criterion id")
	}
	if !strings.Contains(result.Content, "not-a-uuid") {
		t.Errorf("expected the error to echo the invalid value, got: %s", result.Content)
	}
}
