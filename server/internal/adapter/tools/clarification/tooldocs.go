package clarification

import (
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ask_user's description and parameter prose live in
// catalog/system/tools/ask_user.md now (application/registry.Register fills
// them onto Definition() — see internal/application/registry/tooldocs.go);
// this registers the catalog key so internal/platform/runtime's
// completeness test links the file to this package.
func init() {
	prompt.Define[struct{}]("tool."+domain.AskUserToolName, struct{}{})
}
