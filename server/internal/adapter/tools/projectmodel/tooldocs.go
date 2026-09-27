package projectmodel

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		getBriefToolName,
		listChecksToolName,
		listLinksToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
