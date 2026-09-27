package memory

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		saveMemoryToolName,
		searchMemoryToolName,
		deleteMemoryToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
