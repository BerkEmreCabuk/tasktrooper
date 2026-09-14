package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const clickToolName = "browser_click"

type clickArgs struct {
	Selector string `json:"selector"`
}

type clickTool struct {
	session *Session
}

func newClickTool(session *Session) port.ToolExecutor {
	return &clickTool{session: session}
}

func (t *clickTool) Name() string {
	return clickToolName
}

func (t *clickTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        clickToolName,
			Description: "Click the first element matching a CSS selector on the current page of the shared browser. Waits for the element to become visible first.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element to click",
					},
				},
				"required": []string{"selector"},
			},
		},
	}
}

func (t *clickTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a clickArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return toolError(clickToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if a.Selector == "" {
		return toolError(clickToolName, "selector is required")
	}

	// Where the click landed comes back with it: a click that navigates and a
	// click that did nothing visible are the same three words otherwise, and the
	// agent's next read is against whichever page this left behind.
	var url, title string
	err := t.session.run(ctx, interactWaitWindow,
		chromedp.WaitVisible(a.Selector, chromedp.ByQuery),
		chromedp.Click(a.Selector, chromedp.ByQuery),
		chromedp.Location(&url),
		chromedp.Title(&title),
	)
	if err != nil {
		return explainSelectorFailure(ctx, t.session, clickToolName, "click", a.Selector, err)
	}

	return domain.ToolResult{
		Name:    clickToolName,
		Content: fmt.Sprintf("clicked %q\nurl: %s\ntitle: %s", a.Selector, url, title),
		IsError: false,
	}
}
