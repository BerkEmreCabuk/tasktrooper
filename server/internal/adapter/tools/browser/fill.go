package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const fillToolName = "browser_fill"

type fillArgs struct {
	Selector string `json:"selector"`
	Value    string `json:"value"`
	Submit   bool   `json:"submit"`
}

type fillTool struct {
	session *Session
}

func newFillTool(session *Session) port.ToolExecutor {
	return &fillTool{session: session}
}

func (t *fillTool) Name() string {
	return fillToolName
}

func (t *fillTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        fillToolName,
			Description: "Clear an input or textarea matching a CSS selector on the current page of the shared browser and type a value into it. Optionally press Enter afterwards to submit.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the input to fill",
					},
					"value": map[string]interface{}{
						"type":        "string",
						"description": "Text to type into the input",
					},
					"submit": map[string]interface{}{
						"type":        "boolean",
						"description": "Press Enter after typing (default: false)",
					},
				},
				"required": []string{"selector", "value"},
			},
		},
	}
}

func (t *fillTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a fillArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return toolError(fillToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if a.Selector == "" {
		return toolError(fillToolName, "selector is required")
	}

	actions := []chromedp.Action{
		chromedp.WaitVisible(a.Selector, chromedp.ByQuery),
		chromedp.Clear(a.Selector, chromedp.ByQuery),
		chromedp.SendKeys(a.Selector, a.Value, chromedp.ByQuery),
	}
	if a.Submit {
		actions = append(actions, chromedp.SendKeys(a.Selector, kb.Enter, chromedp.ByQuery))
	}
	// Same reason as browser_click: a submit that navigated and a submit that was
	// swallowed read identically without the page the tab ended on.
	var url, title string
	actions = append(actions, chromedp.Location(&url), chromedp.Title(&title))

	if err := t.session.run(ctx, interactWaitWindow, actions...); err != nil {
		return explainSelectorFailure(ctx, t.session, fillToolName, "fill", a.Selector, err)
	}

	content := fmt.Sprintf("filled %q", a.Selector)
	if a.Submit {
		content += " and pressed Enter"
	}
	content += fmt.Sprintf("\nurl: %s\ntitle: %s", url, title)
	return domain.ToolResult{
		Name:    fillToolName,
		Content: content,
		IsError: false,
	}
}
