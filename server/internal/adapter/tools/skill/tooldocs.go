package skill

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

func init() {
	for _, name := range []string{
		loadSkillToolName,
		createSkillToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
