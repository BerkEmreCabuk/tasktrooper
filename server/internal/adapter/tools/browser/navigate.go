package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chromedp/chromedp"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const navigateToolName = "browser_navigate"

// summaryMaxChars bounds the page-text preview so a long landing page does not
// crowd the context; browser_read_dom exists for the full text.
const summaryMaxChars = 1500

type navigateArgs struct {
	URL string `json:"url"`
}

type navigateTool struct {
	session *Session
}

func newNavigateTool(session *Session) port.ToolExecutor {
	return &navigateTool{session: session}
}

func (t *navigateTool) Name() string {
	return navigateToolName
}

func (t *navigateTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        navigateToolName,
			Description: "Open a URL in the shared headless browser. Returns the page title, the final URL after redirects and a short text summary of the page. The browser session persists across browser_* calls, so later clicks and screenshots act on this page.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The full URL to open (must start with http:// or https://)",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

// navigateError is the only thing this tool says about a failure. A missing
// chromium is the one exception, because that is an operator misconfiguration
// the agent must relay rather than retry; everything else — a refused
// destination, a refused connection, a name that does not resolve, a page that
// never finished loading — reads the same, so the pair of "the model picks the
// URL" and "the model reads the error" cannot be turned into a port scanner.
func navigateError(err error) domain.ToolResult {
	if errors.Is(err, errChromeNotFound) {
		return toolError(navigateToolName, chromeNotFoundMsg)
	}
	log.Debug().Err(err).Msg("browser_navigate failed")
	return toolError(navigateToolName, navFailedMsg)
}

func (t *navigateTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a navigateArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return toolError(navigateToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if a.URL == "" {
		return toolError(navigateToolName, "url is required")
	}

	// The full guard, before anything is dialled: scheme allowlist, address
	// rules, and — the part the old lexical check could not do — resolution, so
	// a hostname whose A record is 169.254.169.254 or 10.0.0.5 is refused here
	// rather than fetched and then noticed. The encodings chromium's URL parser
	// accepts and net.ParseIP does not (2852039166, 0xA9FEA9FE,
	// 0251.0376.0251.0376) are refused for the same reason: they are names as
	// far as this check is concerned, and they are judged by what they resolve
	// to, not by how they are spelled.
	target, err := t.session.policy.Validate(ctx, a.URL)
	if err != nil {
		log.Debug().Err(err).Str("url", urlguard.LogRaw(a.URL)).Msg("browser_navigate destination refused")
		return toolError(navigateToolName, navFailedMsg)
	}

	log.Debug().Str("url", urlguard.LogValue(target.URL)).Msg("browser navigate")

	// Redirects and any later self-navigation are not this call's problem any
	// more: Session.run re-checks where the tab ended up, blanks it when the
	// answer is not permitted, and fails the call.
	var title, finalURL, bodyText string
	err = t.session.run(ctx, executeTimeout,
		chromedp.Navigate(target.URL.String()),
		chromedp.Title(&title),
		chromedp.Location(&finalURL),
		chromedp.Text("body", &bodyText, chromedp.ByQuery),
	)
	if err != nil {
		return navigateError(err)
	}

	return domain.ToolResult{
		Name:    navigateToolName,
		Content: fmt.Sprintf("title: %s\nurl: %s\n\n%s", title, finalURL, truncate(bodyText, summaryMaxChars)),
		IsError: false,
	}
}
