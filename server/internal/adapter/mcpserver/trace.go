package mcpserver

import (
	"context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// tracePreviewMax bounds a tool result written to the run trace. The same 500
// the agent loop uses: the trace is read by a human and by the findings digest,
// neither of which wants a whole build log.
const tracePreviewMax = 500

// This endpoint is where a TaskTrooper tool called by a Claude Code session
// actually executes, so it is where that call is written into the run's trace.
//
// The split is deliberate and it is the only one that gives "exactly once".
// adapter/agentcli/claudecode sees the same call go past on the CLI's stream and
// deliberately says nothing about it (see recordedElsewhere there): recording on
// both sides would double every board tool a session used — two entries in the
// ledger the grounding gates read and a duplicated pair in the transcript. But
// "recorded elsewhere" was, until this file existed, recorded NOWHERE: the
// registry writes the tool-usage counters and the session action ledger, and
// neither of those is the activity trace. A session that moved its card, ticked
// a criterion and commented on its PR left no trace step for any of it.
//
// The step types and payload shapes are the agent loop's, byte for byte
// (application/agent/loop.go), because the point is that one renderer serves
// both agent kinds. A claude_code tool call must be indistinguishable from a
// loop tool call once it is in the trace.
func traceStart(ctx context.Context, callID, name, arguments string) {
	if ctx == nil {
		return
	}
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("tool_call_start", map[string]string{
			"tool": name, "call_id": callID, "arguments": arguments,
		})
	}
}

func traceResult(ctx context.Context, callID, name, content string, isError bool) {
	if ctx == nil {
		return
	}
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("tool_call_result", map[string]any{
			"tool": name, "call_id": callID,
			"content":  domain.TruncateHead(content, tracePreviewMax),
			"is_error": isError,
		})
	}
}

// refusalText is what a refused call put in front of the model, so the trace
// records the same sentence the session was given rather than a bare "denied".
func refusalText(result callToolResult) string {
	var sb strings.Builder
	for _, block := range result.Content {
		sb.WriteString(block.Text)
	}
	return sb.String()
}
