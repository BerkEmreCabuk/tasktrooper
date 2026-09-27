package shell_test

import (
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TestShellToolDefinitionsCarryNoGoProse enforces the migration is actually
// done — see board/no_go_prose_test.go (WP8a). timeout_seconds is exempted:
// its description is genuinely per-instance (operator-configured
// timeout/maxTimeout), Go-rendered from a catalog template with live data
// rather than filled by withCatalogDocs — see tooldocs.go.
func TestShellToolDefinitionsCarryNoGoProse(t *testing.T) {
	for _, ex := range []port.ToolExecutor{shell.New("/tmp", 60*time.Second, 15*time.Minute, domain.TerminalSandboxConfig{})} {
		def := ex.Definition()
		if def.Function.Description != "" {
			t.Errorf("%s: Definition() still returns a Go-literal description: %q", ex.Name(), def.Function.Description)
		}
		props, _ := def.Function.Parameters["properties"].(map[string]interface{})
		for name, raw := range props {
			if name == "timeout_seconds" {
				continue
			}
			node, _ := raw.(map[string]interface{})
			if desc, ok := node["description"].(string); ok && desc != "" {
				t.Errorf("%s: parameter %q still carries a Go-literal description: %q", ex.Name(), name, desc)
			}
		}
	}
}
