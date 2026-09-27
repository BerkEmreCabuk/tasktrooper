package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// TestEveryBoardToolHasACatalogDoc is the direction internal/platform/runtime's
// general completeness test does not cover: that one, run from the catalog
// side, only catches an orphaned tools/*.md file or a Define with no
// renderable file. This one starts from the tools this package actually
// registers and requires each to have a catalog/system/tools/<name>.md — the
// gap a stale or missing file for a REAL tool would otherwise leave open.
func TestEveryBoardToolHasACatalogDoc(t *testing.T) {
	for _, ex := range NewExecutors(fullStubKit()) {
		if _, ok := prompt.Default().ToolDoc(ex.Name()); !ok {
			t.Errorf("%s has no catalog/system/tools/%s.md", ex.Name(), ex.Name())
		}
	}
}

// TestToolDocParamPathsMatchSchema guards the OTHER side of that file: a
// tools/<name>.md params path that no longer matches the tool's current JSON
// schema (a renamed or removed property) would otherwise fail silently —
// application/registry.Register just skips a path it cannot resolve.
func TestToolDocParamPathsMatchSchema(t *testing.T) {
	for _, ex := range NewExecutors(fullStubKit()) {
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
