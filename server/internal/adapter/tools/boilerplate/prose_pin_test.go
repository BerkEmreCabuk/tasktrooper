package boilerplate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCatalogHintProseUnchanged pins the exact "hint" field search_boilerplate_catalog
// hands back to the model, ahead of moving it into
// catalog/system/prompts/tool_results/boilerplate_catalog_hint.md.
func TestCatalogHintProseUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleCatalog))
	}))
	defer srv.Close()

	tool := newToolWithServer(t, srv, "acme/boilerplates")
	result := tool.Execute(context.Background(), `{}`)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var payload struct {
		Hint string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "To use an entry, copy its 'path' directory from the repo above as your project's starting point, then adapt names/config — don't regenerate its files from scratch."
	if payload.Hint != want {
		t.Errorf("hint = %q, want %q", payload.Hint, want)
	}
}
