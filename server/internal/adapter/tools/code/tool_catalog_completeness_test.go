package code_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// See board/tool_catalog_completeness_test.go (WP8a) for why these two
// checks exist alongside internal/platform/runtime's general completeness
// test.
func TestEveryCodeToolHasACatalogDoc(t *testing.T) {
	for _, ex := range allExecutors() {
		if _, ok := prompt.Default().ToolDoc(ex.Name()); !ok {
			t.Errorf("%s has no catalog/system/tools/%s.md", ex.Name(), ex.Name())
		}
	}
}

func TestCodeToolDocParamPathsMatchSchema(t *testing.T) {
	for _, ex := range allExecutors() {
		doc, ok := prompt.Default().ToolDoc(ex.Name())
		if !ok {
			continue
		}
		def := ex.Definition()
		for path := range doc.Params {
			if err := registry.ValidateToolParamPath(def.Function.Parameters, path); err != nil {
				t.Errorf("%s: catalog params path %q: %v", ex.Name(), path, err)
			}
		}
	}
}
