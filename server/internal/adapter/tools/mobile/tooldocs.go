package mobile

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		launchToolName,
		screenshotToolName,
		readUIToolName,
		waitForToolName,
		tapToolName,
		typeTextToolName,
		swipeToolName,
		pressButtonToolName,
		rotateToolName,
		unlockToolName,
		releaseToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
