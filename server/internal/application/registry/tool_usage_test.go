package registry_test

import (
	"context"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type failingTool struct {
	name string
}

func (f *failingTool) Name() string { return f.name }
func (f *failingTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type:     "function",
		Function: domain.FunctionDefinition{Name: f.name, Description: "always fails"},
	}
}
func (f *failingTool) Execute(_ context.Context, _ string) domain.ToolResult {
	return domain.ToolResult{Name: f.name, Content: "boom", IsError: true}
}

func call(name string) domain.ToolCall {
	return domain.ToolCall{ID: "1", Function: domain.FunctionCall{Name: name, Arguments: "{}"}}
}

func TestToolUsageCountsSuccessfulCalls(t *testing.T) {
	reg := registry.New()
	reg.Register(&stubTool{name: "grep_code"})
	ctx, usage := registry.ContextWithToolUsage(context.Background())

	reg.Execute(ctx, call("grep_code"))
	reg.Execute(ctx, call("grep_code"))

	if got := usage.Count("grep_code"); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	if !usage.UsedAny("codebase_search", "grep_code") {
		t.Fatal("UsedAny must match a tool that ran")
	}
	if usage.UsedAny("get_repo_tree") {
		t.Fatal("UsedAny must not match a tool that never ran")
	}
}

func TestToolUsageIgnoresFailedAndUnknownCalls(t *testing.T) {
	reg := registry.New()
	reg.Register(&failingTool{name: "grep_code"})
	ctx, usage := registry.ContextWithToolUsage(context.Background())

	// A grep that errored proves nothing was read, and an unknown tool never
	// ran at all — neither may count as evidence the repository was explored.
	reg.Execute(ctx, call("grep_code"))
	reg.Execute(ctx, call("get_repo_tree"))

	if usage.UsedAny("grep_code", "get_repo_tree") {
		t.Fatal("failed and unknown calls must not be recorded")
	}
}

func TestToolUsageWithoutTrackerIsSafe(t *testing.T) {
	reg := registry.New()
	reg.Register(&stubTool{name: "grep_code"})

	// No tracker installed: executing must not panic, and queries answer "no".
	reg.Execute(context.Background(), call("grep_code"))

	var missing *registry.ToolUsage
	if missing.Count("grep_code") != 0 || missing.UsedAny("grep_code") {
		t.Fatal("nil tracker must answer zero")
	}
}
