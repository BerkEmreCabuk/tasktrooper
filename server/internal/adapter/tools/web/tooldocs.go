package web

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{ToolName, DownloadToolName, HTTPRequestToolName} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
