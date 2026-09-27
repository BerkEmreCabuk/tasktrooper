package shell_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const goldenToolDefinitionsPath = "testdata/tool_definitions.golden.json"

// TestGoldenToolDefinitionsSurviveCatalogMigration pins the JSON this
// package's tool presents to an LLM, decorated through registry.Register
// exactly the way platform/runtime wires the real registry — see
// board/golden_tool_definitions_test.go (WP8a) for the full rationale. The
// same timeout/maxTimeout as gendocs_test.go's TestGenerateToolDocs, since
// timeout_seconds' description is rendered from those live values (see
// tooldocs.go) rather than pulled from the catalog.
func TestGoldenToolDefinitionsSurviveCatalogMigration(t *testing.T) {
	reg := registry.New()
	for _, ex := range []port.ToolExecutor{shell.New("/tmp", 60*time.Second, 15*time.Minute, domain.TerminalSandboxConfig{})} {
		reg.Register(ex)
	}

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
