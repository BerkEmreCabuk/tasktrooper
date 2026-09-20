package mcpserver

import (
	"context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const tracePreviewMax = 500

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

func refusalText(result callToolResult) string {
	var sb strings.Builder
	for _, block := range result.Content {
		sb.WriteString(block.Text)
	}
	return sb.String()
}
