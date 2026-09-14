package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The application layer stacks several RoleSystem messages — agent persona +
// skills + rules, KPI and memory blocks, workspace note, language rule, project
// context. Both providers take a single system field, so a translation that
// assigns instead of accumulating silently drops the agent's whole instruction
// set and the model answers as a generic assistant.
var stackedSystemMessages = []domain.Message{
	{Role: domain.RoleSystem, Content: "You are backend-developer."},
	{Role: domain.RoleSystem, Content: "Skill: tdd-workflow."},
	{Role: domain.RoleSystem, Content: "Rule: never push to main."},
	{Role: domain.RoleSystem, Content: "Respond in English."},
	{Role: domain.RoleUser, Content: "Add a health endpoint."},
}

func TestBuildAnthropicRequestKeepsEverySystemMessage(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", stackedSystemMessages, nil, false, nil, 0)

	system := anthropicSystemText(t, req)
	for _, want := range []string{
		"You are backend-developer.",
		"Skill: tdd-workflow.",
		"Rule: never push to main.",
		"Respond in English.",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt lost %q\ngot: %q", want, system)
		}
	}
	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d, want 1 (system messages must not become turns)", len(req.Messages))
	}
}

func TestBuildAnthropicRequestSkipsEmptySystemMessages(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleSystem, Content: "First."},
		{Role: domain.RoleSystem, Content: "   "},
		{Role: domain.RoleSystem, Content: "Second."},
		{Role: domain.RoleUser, Content: "hi"},
	}, nil, false, nil, 0)

	if got := anthropicSystemText(t, req); got != "First.\n\nSecond." {
		t.Errorf("system = %q, want blank entries dropped and the rest joined", got)
	}
}

// lateSystemHistory is the shape the agent loop actually produces: a persona
// head, a couple of real turns, then a system message the loop injected mid-run
// (budget warning, empty-turn nudge).
var lateSystemHistory = []domain.Message{
	{Role: domain.RoleSystem, Content: "You are backend-developer."},
	{Role: domain.RoleUser, Content: "Fix the build."},
	{Role: domain.RoleAssistant, Content: "On it."},
	{Role: domain.RoleSystem, Content: "You have 2 turns left."},
}

// A late system message must stay where it happened. Hoisting it into the
// system field turned a one-off event into a standing rule AND rewrote the
// prompt's first bytes, which invalidates the cached prefix for the rest of the
// run — the single most expensive thing this builder can do.
func TestBuildAnthropicRequestKeepsLateSystemMessagesInPosition(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", lateSystemHistory, nil, false, nil, 0)

	if got := anthropicSystemText(t, req); got != "You are backend-developer." {
		t.Errorf("system = %q, want the persona head only", got)
	}

	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (user, assistant, wrapped reminder)", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || req.Messages[0].Content[0].Text != "Fix the build." {
		t.Errorf("message 0 = %+v, want the user turn", req.Messages[0])
	}
	if req.Messages[1].Role != "assistant" || req.Messages[1].Content[0].Text != "On it." {
		t.Errorf("message 1 = %+v, want the assistant turn", req.Messages[1])
	}

	last := req.Messages[2]
	if last.Role != "user" {
		t.Errorf("late system message role = %q, want user", last.Role)
	}
	want := "<system-reminder>\nYou have 2 turns left.\n</system-reminder>"
	if got := last.Content[0].Text; got != want {
		t.Errorf("reminder text = %q, want %q", got, want)
	}

	// Chat and ChatStream share this builder, so a streamed run must place the
	// reminder identically — the loop injects budget warnings on both paths.
	streamed := buildAnthropicRequest("claude-sonnet-4", lateSystemHistory, nil, true, nil, 0)
	a, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := json.Marshal(streamed.Messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("streamed messages differ:\nbuffered: %s\nstreamed: %s", a, b)
	}
}

// withoutBreakpoints strips cache_control so a comparison sees the prompt text
// rather than the markers laid over it.
func withoutBreakpoints(m anthropicMessage) anthropicMessage {
	out := m
	out.Content = make([]anthropicContent, len(m.Content))
	copy(out.Content, m.Content)
	for i := range out.Content {
		out.Content[i].CacheControl = nil
	}
	return out
}

// The point of keeping it in position: everything before it is untouched, so
// the cached prefix survives the injection. Byte-compared, because "looks the
// same" is exactly the failure mode that silently costs money.
//
// The rolling breakpoint is excluded from the comparison because it is SUPPOSED
// to move onto the new last message — that is how each turn extends the cache.
// What must not move is a single byte of the prompt itself.
func TestLateSystemMessageLeavesThePrefixByteIdentical(t *testing.T) {
	without := buildAnthropicRequest("claude-sonnet-4", lateSystemHistory[:3], nil, false, nil, 0)
	with := buildAnthropicRequest("claude-sonnet-4", lateSystemHistory, nil, false, nil, 0)

	if a, b := anthropicSystemText(t, without), anthropicSystemText(t, with); a != b {
		t.Errorf("system prefix changed:\nwithout: %q\nwith:    %q", a, b)
	}
	if len(with.Messages) != len(without.Messages)+1 {
		t.Fatalf("messages = %d, want exactly one more than %d", len(with.Messages), len(without.Messages))
	}
	for i := range without.Messages {
		a, err := json.Marshal(withoutBreakpoints(without.Messages[i]))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b, err := json.Marshal(withoutBreakpoints(with.Messages[i]))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(a) != string(b) {
			t.Errorf("message %d changed:\nwithout: %s\nwith:    %s", i, a, b)
		}
	}
}

// The reminder rides in the array, so it must not steal the system field's
// breakpoint — and it becomes the new rolling boundary.
func TestLateSystemMessageDoesNotDisturbBreakpoints(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", lateSystemHistory, twoTools(), false, nil, 3)

	if got := countBreakpoints(t, req); got != 4 {
		t.Errorf("breakpoints = %d, want 4", got)
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Content[len(last.Content)-1].CacheControl == nil {
		t.Error("rolling breakpoint missing from the reminder turn")
	}
}

// A tool chain followed by a mid-run injection: the reminder must land after
// the flushed tool results, not merge into them or jump ahead of them.
func TestLateSystemMessageFollowsFlushedToolResults(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleSystem, Content: "persona"},
		{Role: domain.RoleUser, Content: "read it"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`},
		}}},
		{Role: domain.RoleTool, ToolCallID: "call_1", Content: "package main"},
		{Role: domain.RoleSystem, Content: "You have 1 turn left."},
	}, nil, false, nil, 0)

	if got := anthropicSystemText(t, req); got != "persona" {
		t.Errorf("system = %q, want the persona head only", got)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("messages = %d, want 4 (user, assistant, tool_result, reminder)", len(req.Messages))
	}
	if req.Messages[2].Content[0].Type != "tool_result" {
		t.Errorf("message 2 = %+v, want the tool_result batch", req.Messages[2])
	}
	if !strings.Contains(req.Messages[3].Content[0].Text, "<system-reminder>") {
		t.Errorf("message 3 = %+v, want the reminder after the tool results", req.Messages[3])
	}
}

// Gemini has no in-array system role either, so it follows the same rule.
func TestBuildGeminiContentsKeepsLateSystemMessagesInPosition(t *testing.T) {
	contents, system := buildGeminiContents(lateSystemHistory)

	if system == nil || len(system.Parts) == 0 {
		t.Fatal("system instruction is nil")
	}
	if got := system.Parts[0].Text; got != "You are backend-developer." {
		t.Errorf("system instruction = %q, want the persona head only", got)
	}
	if len(contents) != 3 {
		t.Fatalf("contents = %d, want 3 (user, model, wrapped reminder)", len(contents))
	}
	last := contents[2]
	if last.Role != genai.RoleUser {
		t.Errorf("reminder role = %q, want user", last.Role)
	}
	want := "<system-reminder>\nYou have 2 turns left.\n</system-reminder>"
	if got := last.Parts[0].Text; got != want {
		t.Errorf("reminder text = %q, want %q", got, want)
	}
}

func TestBuildGeminiContentsKeepsEverySystemMessage(t *testing.T) {
	contents, system := buildGeminiContents(stackedSystemMessages)

	if system == nil || len(system.Parts) == 0 {
		t.Fatal("system instruction is nil")
	}
	text := system.Parts[0].Text
	for _, want := range []string{
		"You are backend-developer.",
		"Skill: tdd-workflow.",
		"Rule: never push to main.",
		"Respond in English.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("system instruction lost %q\ngot: %q", want, text)
		}
	}
	if len(contents) != 1 {
		t.Fatalf("contents = %d, want 1 (system messages must not become turns)", len(contents))
	}
}

// No system message at all must stay nil rather than becoming an empty
// instruction, which some providers reject.
func TestBuildGeminiContentsWithoutSystemMessage(t *testing.T) {
	_, system := buildGeminiContents([]domain.Message{{Role: domain.RoleUser, Content: "hi"}})
	if system != nil {
		t.Errorf("system instruction = %+v, want nil", system)
	}
}
