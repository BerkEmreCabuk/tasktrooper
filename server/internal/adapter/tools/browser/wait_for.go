package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const waitForToolName = "browser_wait_for"

type waitForArgs struct {
	Selector       string `json:"selector"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type waitForTool struct {
	session *Session
}

func newWaitForTool(session *Session) port.ToolExecutor {
	return &waitForTool{session: session}
}

func (t *waitForTool) Name() string {
	return waitForToolName
}

func (t *waitForTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        waitForToolName,
			Description: "Wait until an element matching a CSS selector is visible on the current page of the shared browser. Use after navigation or a click that triggers async rendering.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element to wait for",
					},
					"timeout_seconds": map[string]interface{}{
						"type":        "integer",
						"description": "How long to wait before giving up (default: 10, max: 30)",
					},
				},
				"required": []string{"selector"},
			},
		},
	}
}

func (t *waitForTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a waitForArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return toolError(waitForToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if a.Selector == "" {
		return toolError(waitForToolName, "selector is required")
	}
	timeout := time.Duration(a.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if timeout > executeTimeout {
		timeout = executeTimeout
	}

	err := t.session.run(ctx, timeout, chromedp.WaitVisible(a.Selector, chromedp.ByQuery))
	if err != nil {
		// A wait that ran out is the case where "why" matters most: an element
		// that never appeared and one that appeared but stayed hidden are two
		// different bugs, and the timeout alone tells them apart for nobody.
		return explainSelectorFailure(ctx, t.session, waitForToolName,
			fmt.Sprintf("waiting %s for", timeout), a.Selector, err)
	}

	return domain.ToolResult{
		Name:    waitForToolName,
		Content: fmt.Sprintf("element %q is visible", a.Selector),
		IsError: false,
	}
}
