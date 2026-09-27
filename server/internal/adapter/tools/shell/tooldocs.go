package shell

import (
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

func init() {
	prompt.Define[struct{}]("tool."+ToolName, struct{}{})
}

type timeoutSecondsInput struct {
	Default int
	Max     int
}

// timeoutSecondsDescKey renders run_terminal's timeout_seconds description.
// Unlike every other field on this tool (filled from catalog/system/tools/
// run_terminal.md by application/registry.withCatalogDocs), this one is
// genuinely per-instance: default/max come from operator config
// (cfg.Tools.Terminal.Timeout/MaxTimeout), not fixed prose — so Definition()
// renders it itself, with live data, from
// catalog/system/prompts/tools/run_terminal_timeout_seconds.md.
var timeoutSecondsDescKey = prompt.Define("tools.run_terminal_timeout_seconds", timeoutSecondsInput{Default: 60, Max: 900})

func timeoutSecondsDescription(timeout, maxTimeout time.Duration) string {
	return timeoutSecondsDescKey.Render(timeoutSecondsInput{
		Default: int(timeout.Seconds()),
		Max:     int(maxTimeout.Seconds()),
	})
}
