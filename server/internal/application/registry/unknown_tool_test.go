package registry

import (
	"strings"
	"testing"
)

// The run this came from: the model called delete_lines, got a bare "unknown
// tool", and reached for delete_file — which removed the whole file it meant to
// take a function out of.
func TestUnknownToolMessagePointsAtTheRealTool(t *testing.T) {
	msg := unknownToolMessage("delete_lines", []string{"read_file", "edit_lines", "delete_file", "write_file"})

	if !strings.Contains(msg, "Did you mean edit_lines?") {
		t.Fatalf("expected edit_lines suggestion, got %q", msg)
	}
	if !strings.Contains(msg, "read_file") || !strings.Contains(msg, "write_file") {
		t.Fatalf("expected the available tools to be listed, got %q", msg)
	}
}

func TestUnknownToolMessageOmitsUnrelatedSuggestion(t *testing.T) {
	msg := unknownToolMessage("zzzzzzzz", []string{"read_file", "edit_lines"})
	if strings.Contains(msg, "Did you mean") {
		t.Fatalf("expected no suggestion for an unrelated name, got %q", msg)
	}
}

func TestUnknownToolMessageWithNoTools(t *testing.T) {
	msg := unknownToolMessage("edit_lines", nil)
	if !strings.Contains(msg, "No tools are available") {
		t.Fatalf("unexpected message: %q", msg)
	}
}
