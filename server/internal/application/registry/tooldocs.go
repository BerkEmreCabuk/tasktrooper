package registry

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// withCatalogDocs wraps executor so every Definition() call is filled from
// its catalog/system/tools/<name>.md doc (see that file's README) before it
// reaches an LLM. A tool with no doc yet — not every package has migrated —
// is returned completely unchanged, which is what lets this run against
// every registered tool regardless of migration state.
//
// The fill happens on every call rather than once at Register time so a
// Definition() built dynamically per call (none do today, but nothing stops
// one) is still filled correctly.
func withCatalogDocs(executor port.ToolExecutor) port.ToolExecutor {
	return &docFilledExecutor{ToolExecutor: executor}
}

type docFilledExecutor struct {
	port.ToolExecutor
}

func (d *docFilledExecutor) Definition() domain.ToolDefinition {
	def := d.ToolExecutor.Definition()
	doc, ok := prompt.Default().ToolDoc(d.Name())
	if !ok {
		return def
	}
	if def.Function.Description == "" {
		def.Function.Description = doc.Description
	}
	// Cloned before any write: a property schema is sometimes a package-level
	// var SHARED across several tools' Parameters (e.g. board's
	// taskRefProperty) — writing into it in place would leak one tool's fill
	// into every other tool that happens to alias the same map, and would
	// make a "raw" ToolExecutor.Definition() stop being side-effect-free.
	if cloned, ok := cloneSchema(def.Function.Parameters).(map[string]interface{}); ok {
		def.Function.Parameters = cloned
	}
	for path, text := range doc.Params {
		// A path the current schema no longer has is a stale catalog entry,
		// not a reason to hide the rest of the tool from the model — see
		// TestToolDocParamPathsMatchSchema (adapter/tools/**) for the test
		// that catches this at build time instead.
		if node, err := resolveSchemaPath(def.Function.Parameters, path); err == nil {
			if node["description"] == nil || node["description"] == "" {
				node["description"] = text
			}
		}
	}
	return def
}

// cloneSchema deep-copies every map[string]interface{} it finds (recursing
// through "items"/"properties" nesting); everything else (strings, slices
// such as "enum"/"required", …) is returned as-is since Definition() never
// writes into those.
func cloneSchema(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	out := make(map[string]interface{}, len(m))
	for k, val := range m {
		out[k] = cloneSchema(val)
	}
	return out
}

// resolveSchemaPath walks path — a dot-separated list of JSON schema keys,
// starting from parameters' "properties" map — to the schema node the last
// segment names. See catalog/system/README.md, "Tool docs", for the syntax
// and worked examples (a bare property name, and the "items"/"properties"
// keywords that reach further into nested array/object schemas).
func resolveSchemaPath(parameters map[string]interface{}, path string) (map[string]interface{}, error) {
	if parameters == nil {
		return nil, fmt.Errorf("no parameters schema")
	}
	props, ok := asSchemaMap(parameters["properties"])
	if !ok {
		return nil, fmt.Errorf("parameters has no properties map")
	}
	segments := splitSchemaPath(path)
	if len(segments) == 0 {
		return nil, fmt.Errorf("empty path")
	}
	cur := props
	for i, seg := range segments {
		raw, ok := cur[seg]
		if !ok {
			return nil, fmt.Errorf("no %q in schema at segment %d of %q", seg, i, path)
		}
		node, ok := asSchemaMap(raw)
		if !ok {
			return nil, fmt.Errorf("%q is not a schema object at segment %d of %q", seg, i, path)
		}
		if i == len(segments)-1 {
			return node, nil
		}
		cur = node
	}
	return nil, fmt.Errorf("unreachable")
}

// ValidateToolParamPath reports whether path resolves against parameters —
// exported so adapter/tools/** tests can assert every tools/<name>.md
// params path still matches that tool's current JSON schema.
func ValidateToolParamPath(parameters map[string]interface{}, path string) error {
	_, err := resolveSchemaPath(parameters, path)
	return err
}

func splitSchemaPath(path string) []string {
	var out []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			out = append(out, path[start:i])
			start = i + 1
		}
	}
	out = append(out, path[start:])
	return out
}

func asSchemaMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}
