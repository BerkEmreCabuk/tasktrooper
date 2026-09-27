package shell_test

import (
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// See board/tool_catalog_completeness_test.go (WP8a) for why these two
// checks exist alongside internal/platform/runtime's general completeness
// test.
func TestEveryShellToolHasACatalogDoc(t *testing.T) {
	for _, ex := range []port.ToolExecutor{shell.New("/tmp", 60*time.Second, 15*time.Minute, domain.TerminalSandboxConfig{})} {
		if _, ok := prompt.Default().ToolDoc(ex.Name()); !ok {
			t.Errorf("%s has no catalog/system/tools/%s.md", ex.Name(), ex.Name())
		}
	}
}

func TestShellToolDocParamPathsMatchSchema(t *testing.T) {
	for _, ex := range []port.ToolExecutor{shell.New("/tmp", 60*time.Second, 15*time.Minute, domain.TerminalSandboxConfig{})} {
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
