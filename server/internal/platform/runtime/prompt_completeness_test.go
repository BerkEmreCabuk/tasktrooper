package runtime

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// TestPromptLibraryCompleteness links every package that calls
// prompt.Define (this package, internal/platform/runtime, imports the whole
// wiring graph) and checks two things against the actual embedded
// catalog/system tree: every Go-side Define call has a matching, renderable
// file, and every prompt/guard/tool file in the catalog has a matching
// Define call. Nothing is migrated yet, so this passes on an empty
// catalog/system — the moment the first file lands under prompts/, guards/
// or tools/ without a Define, or a Define ships with no file, this fails
// naming which.
func TestPromptLibraryCompleteness(t *testing.T) {
	lib, err := prompt.LoadFS(catalog.SystemFS())
	if err != nil {
		t.Fatalf("catalog/system failed to load: %v", err)
	}

	defined := make(map[string]bool)
	for _, k := range prompt.DefinedKeys() {
		defined[k.Name] = true
		if _, err := k.RenderWith(lib); err != nil {
			t.Errorf("Define(%q, ...): sample does not render against catalog/system: %v", k.Name, err)
		}
	}

	for _, entry := range lib.Keys() {
		if entry.Kind == "partial" {
			continue
		}
		if !defined[entry.Name] {
			t.Errorf("catalog/system has %q (%s) with no matching prompt.Define in Go code", entry.Name, entry.Kind)
		}
	}
}
