package clarification_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/clarification"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// See board's tool_catalog_completeness_test.go for why these three checks
// exist alongside internal/platform/runtime's general completeness test.
func TestAskUserHasACatalogDoc(t *testing.T) {
	tool := clarification.NewAskUserTool()
	if _, ok := prompt.Default().ToolDoc(tool.Name()); !ok {
		t.Fatalf("%s has no catalog/system/tools/%s.md", tool.Name(), tool.Name())
	}
}

func TestAskUserToolDocParamPathsMatchSchema(t *testing.T) {
	tool := clarification.NewAskUserTool()
	doc, ok := prompt.Default().ToolDoc(tool.Name())
	if !ok {
		t.Fatalf("%s has no catalog doc", tool.Name())
	}
	def := tool.Definition()
	for path := range doc.Params {
		if err := registry.ValidateToolParamPath(def.Function.Parameters, path); err != nil {
			t.Errorf("%s: catalog params path %q: %v", tool.Name(), path, err)
		}
	}
}
