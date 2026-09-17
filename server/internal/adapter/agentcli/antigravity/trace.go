package antigravity

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

// nativeToolNames maps AGY's own tool identifiers (upd.ToolName in
// stream.go) onto the TaskTrooper tool that does the same job — see
// claudecode's identical map for why this is not cosmetic.
//
// LOW CONFIDENCE, more so than cursor's or opencode's map: AGY's public docs
// and third-party writeups disagree on its exact native tool names (sources
// cite read_file, view_file, code_search and grep_search as apparently
// overlapping read/search tools; write_file, write_to_file and
// replace_file_content as overlapping write tools), and none of it is
// verified against this package's own stream-json output on a live session.
// Every variant a source named is included so a real match still lands on
// the right ledger entry; an unmapped tool is still recorded under its own
// raw name (see ledgerToolName), which satisfies no gate but is truthful and
// still counts toward the run's total. Treat this map as a starting point to
// correct once AGY's actual tool_name values are seen in a real trace.
var nativeToolNames = map[string]string{
	"read_file":                  "read_file",
	"view_file":                  "read_file",
	"code_search":                "codebase_search",
	"grep_search":                "grep_code",
	"list_directory":             "get_repo_tree",
	"glob":                       "get_repo_tree",
	"write_file":                 "write_file",
	"write_to_file":              "write_file",
	"replace_file_content":       "edit_file",
	"multi_replace_file_content": "edit_file",
	"run_command":                "run_terminal",
	"run_shell_command":          "run_terminal",
	"read_url":                   "fetch_url",
	"google_web_search":          "web_search",
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

// traceSink writes the AGY session into the two places a loop run writes
// itself: the activity trace the UI streams, and the tool ledger the board's
// grounding gates read. See claudecode's identical traceSink for the full
// reasoning; this is the same shape adapted to AGY's sink interface.
type traceSink struct {
	ctx     context.Context
	taskKey string

	mu    sync.Mutex
	turns int
}

func (t *traceSink) OnSession(sessionID, model string) {
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("antigravity_session", map[string]string{
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
