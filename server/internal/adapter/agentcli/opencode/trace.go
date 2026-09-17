package opencode

import (
	"context"
	"strings"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// tracePreviewMax mirrors claudecode's own preview budget for a tool result
// written to the run trace.
const tracePreviewMax = 500

// nativeToolNames maps opencode's own tool identifiers (toolPart.Tool in
// stream.go, a plain string field — not the dynamically-keyed wrapper
// cursor-agent uses, so there is no "function" fallback ambiguity here) onto
// the TaskTrooper tool that does the same job — see claudecode's identical
// map for why this is not cosmetic.
//
// Sourced from opencode's own documented built-in tool set (opencode.ai/docs/
// tools/): read, write, edit, grep, glob, bash, webfetch, websearch. Not
// independently verified against a live session in this repo — an unmapped
// tool is still recorded under its own raw name (see ledgerToolName), which
// satisfies no gate but is truthful and still counts toward the run's total.
var nativeToolNames = map[string]string{
	"read":      "read_file",
	"write":     "write_file",
	"edit":      "edit_file",
	"grep":      "grep_code",
	"glob":      "get_repo_tree",
	"bash":      "run_terminal",
	"webfetch":  "fetch_url",
	"websearch": "web_search",
}

// ownToolMarker and recordedElsewhere mirror cursor's identical guard against
// double-recording a tool call TaskTrooper's own MCP endpoint already
// recorded when it executed — see cursor/trace.go for the full reasoning.
const ownToolMarker = "tasktrooper"

func recordedElsewhere(nativeName string) bool {
	return strings.Contains(strings.ToLower(nativeName), ownToolMarker)
}

func ledgerToolName(native string) string {
	if mapped, ok := nativeToolNames[native]; ok {
		return mapped
	}
	return native
}

// traceSink writes the opencode session into the two places a loop run
// writes itself: the activity trace the UI streams, and the tool ledger the
// board's grounding gates read. See claudecode's identical traceSink for the
// full reasoning; this is the same shape adapted to opencode's sink
// interface.
type traceSink struct {
	ctx     context.Context
	taskKey string

	mu    sync.Mutex
	turns int
}

func (t *traceSink) OnSession(sessionID, model string) {
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("opencode_session", map[string]string{
			"cli_session_id": sessionID,
			"model":          model,
			"task_key":       t.taskKey,
		})
	}
}

func (t *traceSink) OnTurn() {
	t.mu.Lock()
	t.turns++
	turn := t.turns
	t.mu.Unlock()
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("iteration_start", map[string]any{"iteration": turn})
	}
}

func (t *traceSink) OnAssistantText(text string) {
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("assistant_message", map[string]string{"content": text})
	}
}

func (t *traceSink) OnToolUse(callID, name, arguments string) {
	if recordedElsewhere(name) {
		return
	}
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("tool_call_start", map[string]string{
			"tool": ledgerToolName(name), "call_id": callID, "arguments": arguments,
		})
	}
}

// OnToolResult is the only place the ledger is written — see claudecode's
// identical method for why a request is not counted, only a result.
func (t *traceSink) OnToolResult(callID, name, content string, isError bool) {
	if recordedElsewhere(name) {
		return
	}
	ledgerName := ledgerToolName(name)
	if isError {
		registry.ToolUsageFromContext(t.ctx).RecordError(ledgerName)
	} else {
		registry.ToolUsageFromContext(t.ctx).Record(ledgerName)
	}
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("tool_call_result", map[string]any{
			"tool": ledgerName, "call_id": callID,
			"content":  domain.TruncateHead(content, tracePreviewMax),
			"is_error": isError,
		})
	}
}
