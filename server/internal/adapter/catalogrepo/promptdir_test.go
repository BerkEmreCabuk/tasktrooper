package catalogrepo

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPromptDirOpenReadsSystemSubdir(t *testing.T) {
	dir := t.TempDir()
	sysDir := filepath.Join(dir, "system")
	if err := os.MkdirAll(sysDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sysDir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	fsys, label, err := (&PromptDir{Dir: dir}).Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if label != LocalDirMarker+dir {
		t.Errorf("label = %q, want %q", label, LocalDirMarker+dir)
	}
	raw, err := fs.ReadFile(fsys, "README.md")
	if err != nil {
		t.Fatalf("read README.md via returned fs: %v", err)
	}
	if string(raw) != "hi" {
		t.Errorf("README.md content = %q, want %q", raw, "hi")
	}
}

func TestPromptDirOpenNoSystemDir(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := (&PromptDir{Dir: dir}).Open(context.Background()); err == nil {
		t.Fatal("expected an error when system/ is missing")
	}
}
