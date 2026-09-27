package browser

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		navigateToolName,
		waitForToolName,
		clickToolName,
		fillToolName,
		readDOMToolName,
		screenshotToolName,
		viewportToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
