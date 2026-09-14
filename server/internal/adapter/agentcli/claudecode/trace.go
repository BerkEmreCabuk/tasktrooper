package claudecode

import (
	"context"
	"strings"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// tracePreviewMax bounds a tool result written to the run trace, matching the
// agent loop's own preview budget: the trace is read by a human and by the
// findings digest, neither of which wants a whole build log.
const tracePreviewMax = 500

// nativeToolNames maps the CLI's own tool names onto the TaskTrooper tool that
// does the same job.
//
// This is not cosmetic. The board's grounding gates are built on the tool
// ledger — an analiz run that never read the repository is rejected
// (domain.CodeExplorationTools), a QA run that never exercised the product is
// rejected — and those gates ask for names like read_file and grep_code. A CLI
// session does the same reading under the names Read and Grep, so without this
// map every claude_code analysis run would be failed for work it demonstrably
// did, by a gate that was looking for a different spelling.
//
// Only the equivalences that are actually equivalent are listed. A CLI tool
// with no counterpart (TodoWrite, Task) keeps its own name: it is recorded, it
// counts toward the run's tool total, and it satisfies no gate — which is the
// truthful outcome, because it is evidence of nothing in particular.
var nativeToolNames = map[string]string{
	"Read":      "read_file",
	"Grep":      "grep_code",
	"Glob":      "get_repo_tree",
	"LS":        "get_repo_tree",
	"Bash":      "run_terminal",
	"Edit":      "edit_file",
	"MultiEdit": "edit_file",
	"Write":     "write_file",
	"WebFetch":  "fetch_url",
	"WebSearch": "web_search",
}

// ledgerToolName is the name a CLI tool is counted under.
func ledgerToolName(native string) string {
	if mapped, ok := nativeToolNames[native]; ok {
		return mapped
	}
	return native
}

// ownToolPrefix is how the CLI namespaces the tools THIS server serves it over
// MCP: mcp__<server key>__<tool>, with the key fixed by mcpserver.serverName.
//
// Calls to those are already recorded — the ledger entry and the
// tool_call_start/tool_call_result steps are written by the registry when the
// endpoint executes the call, exactly as they are for a loop run. Counting them
// here as well would double every board tool a session used: two entries in the
// ledger the grounding gates read, and a duplicated pair in the transcript. So
// this half of the pipe stays silent for them and reports only what the CLI did
// on its own (its native tools, and any OTHER MCP server the operator
// configured, which nothing on this side ever sees).
const ownToolPrefix = "mcp__tasktrooper__"

// recordedElsewhere reports a tool whose ledger entry and trace steps are
// written by the registry rather than here.
func recordedElsewhere(nativeName string) bool {
	return strings.HasPrefix(nativeName, ownToolPrefix)
}

// traceSink writes the CLI session into the two places a loop run writes
// itself: the activity trace the UI streams, and the tool ledger the board's
// grounding gates read.
//
// It emits the SAME step types the agent loop emits (assistant_message,
// tool_call_start, tool_call_result) rather than a claude_code-specific
// vocabulary, so the existing transcript, the findings digest
// (application/agent/digest.go) and the UI keep working with no changes on
// their side — which is the point: a task worked by the CLI must read on the
// board exactly like a task worked by the loop.
type traceSink struct {
	ctx     context.Context
	taskKey string

	mu        sync.Mutex
	sessionID string
	model     string
	// turns counts the assistant turns seen so far, which is what numbers the
	// iteration_start steps. Under the mutex with the rest: the stream is read by
	// one goroutine today, and a counter that silently depends on that is the
	// kind of thing a second reader would corrupt without any test noticing.
	turns int
}

func (t *traceSink) OnSession(sessionID, model string) {
	t.mu.Lock()
	t.sessionID, t.model = sessionID, model
	t.mu.Unlock()
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("claude_code_session", map[string]string{
			"cli_session_id": sessionID,
			"model":          model,
			"task_key":       t.taskKey,
		})
	}
}

// OnTurn brackets one assistant turn with the SAME step the agent loop opens an
// iteration with.
//
// Not a claude_code-specific step type, and that is the whole point: the SPA's
// activity graph attaches a turn's narration and tool calls to the iteration
// enclosing them, and a trace with no iterations at all had every tool call
// dropped by the renderer — a session that read, grepped, edited and built
// showed as nothing but its closing sentence. Emitting the bracket the renderer
// already understands is what makes a CLI run read like a loop run, with no
// second code path on either side.
//
// Turns are numbered from 1, like the loop's iterations.
func (t *traceSink) OnTurn() {
	t.mu.Lock()
	t.turns++
	turn := t.turns
	t.mu.Unlock()
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("iteration_start", map[string]any{"iteration": turn})
	}
}

// Turns is how many assistant turns the session produced. Read after the stream
// ends, for the trace's closing step.
func (t *traceSink) Turns() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.turns
}

// Model is what the init event said the session was running on.
//
// It is on the closing step as well as the opening one because that is the step
// a reader lands on: a run whose agent has no model pinned takes whatever the
// operator's CLI defaults to, and "which model actually ran this" is otherwise
// answerable only by expanding the first node of the trace.
func (t *traceSink) Model() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.model
}

// SessionID is read after the stream ends, by the park path: the id is the one
// thing a quota-blocked run must carry out with it.
func (t *traceSink) SessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
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

// OnToolResult is the only place the ledger is written. A tool_use is a
// request; a result is what happened. Counting requests would credit a run for
// a call the session abandoned — and the gates that read this ledger exist
// precisely to tell claimed work from done work.
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
