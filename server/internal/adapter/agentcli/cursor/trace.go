package cursor

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

// nativeToolNames maps cursor-agent's own tool identifiers onto the
// TaskTrooper tool that does the same job — see claudecode's identical map
// for why this is not cosmetic: the board's grounding gates read the ledger
// for names like read_file and grep_code.
//
// Confidence is UNEVEN. cursor-agent's documented stream-json format
// (cursor.com/docs/cli/reference/output-format) names exactly two native
// tools explicitly: "read" and "write" (from the dynamically-keyed
// readToolCall/writeToolCall wrapper, stripped of its "ToolCall" suffix by
// firstToolCall in stream.go). Every OTHER native tool the CLI has is
// documented only as falling back to a generic `tool_call.function` shape
// with its own "name" field — which this package's stream.go does not
// currently unpack (firstToolCall reads the dynamically-keyed wrapper only),
// so those calls arrive here, if at all, under the literal name "function"
// rather than their real identifier. That is a stream.go parsing gap, not
// this map's — fixing it needs the CLI's actual function-call event shape
// verified against a live session, which this map does not attempt.
var nativeToolNames = map[string]string{
	"read":  "read_file",
	"write": "write_file",
}

// ownToolPrefix marks a tool call as one TaskTrooper's own MCP endpoint
// already recorded when it executed — see claudecode's ownToolPrefix for why
// double-recording it here would pollute both the ledger and the trace.
// cursor-agent's own stream-json format documents no fixed naming convention
// for an MCP-served tool's call_id/name the way Claude Code's
// "mcp__<server>__<tool>" prefix is documented, so this checks for the
// server's own name anywhere in the identifier instead of a fixed prefix —
// a false negative here only double-counts a stat, a false positive would
// hide real work, and "tasktrooper" appearing in a native tool's own name is
// not a coincidence this CLI would produce.
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

// traceSink writes the cursor-agent session into the two places a loop run
// writes itself: the activity trace the UI streams, and the tool ledger the
// board's grounding gates read. See claudecode's identical traceSink for the
// full reasoning; this is the same shape adapted to cursor-agent's sink
// interface.
type traceSink struct {
	ctx     context.Context
	taskKey string

	mu    sync.Mutex
	turns int
}

func (t *traceSink) OnSession(sessionID, model string) {
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("cursor_session", map[string]string{
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
