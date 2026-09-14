package llm

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A provider that answers with `"id": null` used to hand the loop a tool result
// with no tool_call_id; the next request came back
// `400 Unexpected tool call id None in tool message` and the run died.
func TestParseToolCallsGivesMissingIDsAnID(t *testing.T) {
	calls := parseToolCalls([]toolCall{
		{Function: functionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}},
		{Function: functionCall{Name: "read_file", Arguments: `{"path":"b.go"}`}},
	})

	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	for _, c := range calls {
		if len(c.ID) != toolCallIDLength {
			t.Errorf("id %q has length %d, want %d alphanumeric chars", c.ID, len(c.ID), toolCallIDLength)
		}
		if c.Type != "function" {
			t.Errorf("type = %q, want function", c.Type)
		}
	}
	if calls[0].ID == calls[1].ID {
		t.Errorf("two calls in one turn share id %q", calls[0].ID)
	}
	if got := parseToolCalls([]toolCall{{Function: functionCall{Name: "read_file"}}}); got[0].ID != calls[0].ID {
		t.Errorf("id is not stable across parses: %q vs %q", got[0].ID, calls[0].ID)
	}
}

func TestParseToolCallsKeepsProviderIDs(t *testing.T) {
	calls := parseToolCalls([]toolCall{{ID: "abc123xyz", Type: "function", Function: functionCall{Name: "grep_code"}}})
	if calls[0].ID != "abc123xyz" {
		t.Errorf("id = %q, want the provider's own id", calls[0].ID)
	}
}

func TestNormalizeToolPairingDropsOrphanToolResult(t *testing.T) {
	// The token budget removed the assistant turn but kept its result.
	got := normalizeToolPairing([]domain.Message{
		{Role: domain.RoleUser, Content: "hi"},
		{Role: domain.RoleTool, Content: "a.go", ToolCallID: "call12345", Name: "read_file"},
		{Role: domain.RoleAssistant, Content: "done"},
	})

	if len(got) != 2 {
		t.Fatalf("messages = %d, want the orphan tool result dropped", len(got))
	}
	for _, m := range got {
		if m.Role == domain.RoleTool {
			t.Error("orphan tool result survived")
		}
	}
}

func TestNormalizeToolPairingDropsUnansweredToolCalls(t *testing.T) {
	// A replayed session stores the assistant's tool calls but never its results.
	got := normalizeToolPairing([]domain.Message{
		{Role: domain.RoleUser, Content: "hi"},
		{Role: domain.RoleAssistant, Content: "looking", ToolCalls: []domain.ToolCall{
			{ID: "call12345", Function: domain.FunctionCall{Name: "read_file"}},
		}},
		{Role: domain.RoleUser, Content: "still there?"},
	})

	if len(got) != 3 {
		t.Fatalf("messages = %d, want 3", len(got))
	}
	if len(got[1].ToolCalls) != 0 {
		t.Error("unanswered tool call survived")
	}
	if got[1].Content != "looking" {
		t.Errorf("content = %q, want the assistant text kept", got[1].Content)
	}
}

func TestNormalizeToolPairingDropsEmptyAssistantWithNoResults(t *testing.T) {
	got := normalizeToolPairing([]domain.Message{
		{Role: domain.RoleUser, Content: "hi"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: "call12345", Function: domain.FunctionCall{Name: "read_file"}},
		}},
	})

	if len(got) != 1 {
		t.Fatalf("messages = %d, want the contentless unanswered assistant turn dropped", len(got))
	}
}

func TestNormalizeToolPairingKeepsMatchedPairs(t *testing.T) {
	in := []domain.Message{
		{Role: domain.RoleUser, Content: "hi"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: "call12345", Function: domain.FunctionCall{Name: "read_file"}},
			{ID: "call67890", Function: domain.FunctionCall{Name: "grep_code"}},
		}},
		{Role: domain.RoleTool, Content: "a", ToolCallID: "call12345", Name: "read_file"},
		{Role: domain.RoleTool, Content: "b", ToolCallID: "call67890", Name: "grep_code"},
		{Role: domain.RoleAssistant, Content: "done"},
	}

	got := normalizeToolPairing(in)

	if len(got) != len(in) {
		t.Fatalf("messages = %d, want %d — valid pairs must survive untouched", len(got), len(in))
	}
	if len(got[1].ToolCalls) != 2 {
		t.Errorf("tool calls = %d, want both kept", len(got[1].ToolCalls))
	}
}

func TestNormalizeToolPairingDropsHalfAnsweredCall(t *testing.T) {
	// The budget trimmer removed one result of a two-call turn.
	got := normalizeToolPairing([]domain.Message{
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: "call12345", Function: domain.FunctionCall{Name: "read_file"}},
			{ID: "call67890", Function: domain.FunctionCall{Name: "grep_code"}},
		}},
		{Role: domain.RoleTool, Content: "b", ToolCallID: "call67890", Name: "grep_code"},
	})

	if len(got) != 2 {
		t.Fatalf("messages = %d, want 2", len(got))
	}
	if len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].ID != "call67890" {
		t.Errorf("tool calls = %+v, want only the answered one", got[0].ToolCalls)
	}
}

func TestBuildChatMessagesEmitsToolCallID(t *testing.T) {
	msgs := buildChatMessages([]domain.Message{
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: "call12345", Type: "function", Function: domain.FunctionCall{Name: "read_file"}},
		}},
		{Role: domain.RoleTool, Content: "a", ToolCallID: "call12345", Name: "read_file"},
	})

	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	if msgs[0].ToolCalls[0].ID != "call12345" {
		t.Errorf("assistant call id = %q", msgs[0].ToolCalls[0].ID)
	}
	if msgs[1].ToolCallID != "call12345" {
		t.Errorf("tool result id = %q, want the call it answers", msgs[1].ToolCallID)
	}
}

// An assistant turn with neither text nor tool calls has no shape on the wire:
// the provider answers `400 Assistant message must have either content or
// tool_calls, but not none` and rejects the whole conversation. The session
// store persists such a turn, so it comes back on every later request — the
// conversation stays dead until this drops it.
func TestBuildChatMessagesDropsEmptyAssistantTurns(t *testing.T) {
	msgs := buildChatMessages([]domain.Message{
		{Role: domain.RoleUser, Content: "delete DE-1"},
		{Role: domain.RoleAssistant, Content: "   "},
		{Role: domain.RoleUser, Content: "did you?"},
	})

	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2: %+v", len(msgs), msgs)
	}
	for _, m := range msgs {
		if m.Role == "assistant" {
			t.Fatalf("empty assistant turn survived: %+v", m)
		}
	}
}

// Only the contentless ones go. An assistant turn that says something, or one
// that carries answered tool calls, is the conversation itself.
func TestBuildChatMessagesKeepsAssistantTurnsThatCarrySomething(t *testing.T) {
	msgs := buildChatMessages([]domain.Message{
		{Role: domain.RoleAssistant, Content: "on it"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "tc1", Function: domain.FunctionCall{Name: "read_file"}}}},
		{Role: domain.RoleTool, ToolCallID: "tc1", Name: "read_file", Content: "package main"},
	})

	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3: %+v", len(msgs), msgs)
	}
}
