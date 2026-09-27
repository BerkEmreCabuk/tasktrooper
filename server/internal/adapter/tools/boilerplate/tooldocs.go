package boilerplate

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	prompt.Define[struct{}]("tool."+ToolName, struct{}{})
}
