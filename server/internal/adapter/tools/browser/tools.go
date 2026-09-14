package browser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func NewExecutors(session *Session) []port.ToolExecutor {
	if session == nil {
		return nil
	}
	return []port.ToolExecutor{
		newNavigateTool(session),
		newWaitForTool(session),
		newClickTool(session),
		newFillTool(session),
		newReadDOMTool(session),
		newScreenshotTool(session),
		newViewportTool(session),
	}
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

// runError keeps the chromium-missing message verbatim — the agent must relay
// it as-is instead of retrying — collapses everything that says something about
// a destination into one flat message, and prefixes the rest with the action.
//
// The split is what keeps the tool useful without keeping the port scanner. "no
// element matching #submit" is a fact about the page under test and the QA agent
// needs it. "net::ERR_CONNECTION_REFUSED" versus "net::ERR_ADDRESS_UNREACHABLE"
// versus "this destination is blocked" is a fact about what is listening where,
// fed back to the model that chose the address — so those three become one
// sentence and the detail goes to the log.
func runError(name, op string, err error) domain.ToolResult {
	if errors.Is(err, errChromeNotFound) {
		return toolError(name, chromeNotFoundMsg)
	}
	if errors.Is(err, errBlockedPage) || isTransportError(err) {
		log.Debug().Err(err).Str("tool", name).Msg("browser tool refused the current page")
		return toolError(name, pageUnavailableMsg)
	}
	return toolError(name, fmt.Sprintf("%s: %v", op, err))
}

// isTransportError reports whether err is chromium describing how a network
// request failed. chromedp surfaces those as "net::ERR_*" inside a page-load
// error, and which ERR it is, is exactly the part the model must not see.
func isTransportError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "net::ERR")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n[truncated at %d bytes]", max)
}
