package boilerplate

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TestBoilerplateToolDefinitionsCarryNoGoProse enforces the migration is
// actually done — see board/no_go_prose_test.go (WP8a).
func TestBoilerplateToolDefinitionsCarryNoGoProse(t *testing.T) {
	for _, ex := range []port.ToolExecutor{New(fakeSettings{})} {
		def := ex.Definition()
		if def.Function.Description != "" {
			t.Errorf("%s: Definition() still returns a Go-literal description: %q", ex.Name(), def.Function.Description)
		}
		assertNoParamProse(t, ex.Name(), def.Function.Parameters)
	}
}

func assertNoParamProse(t *testing.T, toolName string, node map[string]interface{}) {
	t.Helper()
	if node == nil {
		return
	}
	if desc, ok := node["description"].(string); ok && desc != "" {
		t.Errorf("%s: a parameter still carries a Go-literal description: %q", toolName, desc)
	}
	if items, ok := node["items"].(map[string]interface{}); ok {
		assertNoParamProse(t, toolName, items)
	}
	if props, ok := node["properties"].(map[string]interface{}); ok {
		for _, raw := range props {
			if sub, ok := raw.(map[string]interface{}); ok {
				assertNoParamProse(t, toolName, sub)
			}
		}
	}
}
