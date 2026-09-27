package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/clarification"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TestGenerateToolDocs is WP8a's migration script, kept as a test so it shares
// fullStubKit and the board/clarification imports with the tests that check
// its output, rather than duplicating them in a throwaway `go run` program.
//
// It is a DUMP, not a rewrite: it reads each tool's CURRENT Definition() (Go
// prose still in place) and writes catalog/system/tools/<name>.md byte-for-
// byte from it — front matter `params` for every property description, body
// = the function description. Skipped unless GENERATE_TOOL_DOCS=1, so it
// never runs in CI and is never a dependency of the build.
//
// A follow-up migrating another tools/** package (code/, mobile/, ops/,
// browser/, …) copies this file into that package, swaps fullStubKit()/
// NewExecutors for that package's own executor constructor, and runs:
//
//	GENERATE_TOOL_DOCS=1 go test ./internal/adapter/tools/<package>/... -run TestGenerateToolDocs -v
//
// then blanks that package's Definition() Description/"description" fields
// (WP8a did this with a throwaway go/ast rewrite — see the WP8a report for
// the exact approach) and adds one prompt.Define("tool."+name, struct{}{})
// per tool (see board/tooldocs.go) so the completeness test in
// internal/platform/runtime accepts the new catalog files.
func TestGenerateToolDocs(t *testing.T) {
	if os.Getenv("GENERATE_TOOL_DOCS") == "" {
		t.Skip("set GENERATE_TOOL_DOCS=1 to (re)generate catalog/system/tools/*.md from current Go prose")
	}

	dir := repoToolsDir(t)
	execs := append([]port.ToolExecutor{}, NewExecutors(fullStubKit())...)
	execs = append(execs, clarification.NewAskUserTool())

	for _, ex := range execs {
		writeToolDoc(t, dir, ex.Definition())
	}

	writeGoldenToolDefinitions(t, execs)
}

// writeGoldenToolDefinitions (re)writes testdata/tool_definitions.golden.json
// the same way TestGoldenToolDefinitionsSurviveCatalogMigration reads it —
// through registry.Register — so capturing it here and asserting it there
// exercise the identical decorated path.
func writeGoldenToolDefinitions(t *testing.T, execs []port.ToolExecutor) {
	t.Helper()
	reg := registry.New()
	for _, ex := range execs {
		reg.Register(ex)
	}
	defs := reg.Definitions()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Function.Name < defs[j].Function.Name })

	out, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden definitions: %v", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(goldenToolDefinitionsPath, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", goldenToolDefinitionsPath, err)
	}
}

// repoToolsDir finds catalog/system/tools relative to this package
// (server/internal/adapter/tools/board), independent of the caller's cwd.
func repoToolsDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(wd, "..", "..", "..", "..", "..", "catalog", "system", "tools")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("catalog/system/tools not found at %s: %v", dir, err)
	}
	return dir
}

type toolFrontMatter struct {
	Key     string            `yaml:"key"`
	Version string            `yaml:"version"`
	Params  map[string]string `yaml:"params,omitempty"`
}

func writeToolDoc(t *testing.T, dir string, def domain.ToolDefinition) {
	t.Helper()
	name := def.Function.Name
	if name == "" {
		t.Fatalf("tool definition has no name: %+v", def)
	}
	if def.Function.Description == "" {
		// Already migrated (or never had Go prose) — nothing left to dump.
		return
	}

	params := map[string]string{}
	collectParamDescriptions(def.Function.Parameters, params)

	fm := toolFrontMatter{Key: "tool." + name, Version: "1", Params: params}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		t.Fatalf("%s: marshal front matter: %v", name, err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmBytes)
	b.WriteString("---\n")
	b.WriteString(def.Function.Description)
	b.WriteString("\n")

	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("%s: write %s: %v", name, path, err)
	}
}

// collectParamDescriptions walks parameters' "properties" map into path ->
// description text, using EXACTLY the path syntax resolveSchemaPath expects
// (see catalog/system/README.md, "Tool docs"): a property name, optionally
// followed by literal "items"/"properties" schema keys to reach further in.
func collectParamDescriptions(parameters map[string]interface{}, out map[string]string) {
	props, ok := parameters["properties"].(map[string]interface{})
	if !ok {
		return
	}
	for _, name := range sortedKeys(props) {
		node, _ := props[name].(map[string]interface{})
		walkParamNode(node, name, out)
	}
}

func walkParamNode(node map[string]interface{}, path string, out map[string]string) {
	if node == nil {
		return
	}
	if desc, ok := node["description"].(string); ok && desc != "" {
		out[path] = desc
	}
	if items, ok := node["items"].(map[string]interface{}); ok {
		walkParamNode(items, path+".items", out)
	}
	if props, ok := node["properties"].(map[string]interface{}); ok {
		for _, name := range sortedKeys(props) {
			sub, _ := props[name].(map[string]interface{})
			walkParamNode(sub, path+".properties."+name, out)
		}
	}
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
