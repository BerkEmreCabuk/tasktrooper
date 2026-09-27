package board

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/clarification"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

const goldenToolDefinitionsPath = "testdata/tool_definitions.golden.json"

// TestGoldenToolDefinitionsSurviveCatalogMigration pins the JSON every board
// tool and ask_user present to an LLM, decorated through registry.Register
// exactly the way platform/runtime wires the real registry. It was captured
// BEFORE WP8a moved that prose out of Go and into catalog/system/tools/**.md
// (see that file's README) and is never touched again: this test is what
// proves the migration changed nothing an LLM sees, byte for byte.
func TestGoldenToolDefinitionsSurviveCatalogMigration(t *testing.T) {
	reg := registry.New()
	for _, ex := range NewExecutors(fullStubKit()) {
		reg.Register(ex)
	}
	reg.Register(clarification.NewAskUserTool())

	defs := reg.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })

	got, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		t.Fatalf("marshal definitions: %v", err)
	}
	got = append(got, '\n')

	want, err := os.ReadFile(goldenToolDefinitionsPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenToolDefinitionsPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("tool definitions changed byte-for-byte from %s\n--- want ---\n%s\n--- got ---\n%s",
			goldenToolDefinitionsPath, want, got)
	}
}
