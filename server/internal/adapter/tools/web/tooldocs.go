package web

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{ToolName, DownloadToolName} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
