package code

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// TestFileToolProseUnchanged pins the exact instructive wording these tools
// hand back to the model, ahead of moving it into catalog/system/guards and
// catalog/system/prompts/tool_results — see catalog/system/README.md. A
// mismatch here means the migration changed what an LLM reads, not just
// where the sentence lives.
func TestFileToolProseUnchanged(t *testing.T) {
	newCtx := func(root string) context.Context {
		return registry.ContextWithWorkspaceDir(
			registry.ContextWithSessionID(context.Background(), uuid.New()),
			root,
		)
	}
	seed := func(root, name, content string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("edit_file missing old_string", func(t *testing.T) {
		root := t.TempDir()
		got := NewEditFileTool().Execute(newCtx(root), `{"path":"a.ts","old_string":"","new_string":"x"}`).Content
		want := "old_string is required. To create a file or replace it whole, use write_file."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_file no such file", func(t *testing.T) {
		root := t.TempDir()
		got := NewEditFileTool().Execute(newCtx(root), `{"path":"missing.ts","old_string":"a","new_string":"b"}`).Content
		want := "no such file: missing.ts. Paths are relative to the workspace root — use get_repo_tree or grep_code to find it, or write_file to create it."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_file old_string not found", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "const a = 1\n")
		got := NewEditFileTool().Execute(newCtx(root), `{"path":"a.ts","old_string":"     1→const a = 1","new_string":"const a = 2"}`).Content
		want := "old_string was not found in a.ts. Copy it exactly from read_file output — without the line-number prefix, and with the file's own indentation and line breaks."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_file ambiguous match", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "x\nx\nx\n")
		got := NewEditFileTool().Execute(newCtx(root), `{"path":"a.ts","old_string":"x","new_string":"y"}`).Content
		want := "old_string matches 3 places in a.ts. Either pass replace_all:true to change all 3, or include the surrounding lines so it matches exactly one."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_file success note", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "androidSoon\nb\nandroidSoon\n")
		got := NewEditFileTool().Execute(newCtx(root), `{"path":"a.ts","old_string":"androidSoon","new_string":"androidLink","replace_all":true}`).Content
		want := "a.ts: 2 occurrences replaced at line 1, 3. The edit is on disk — do not grep to verify it."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_lines unknown mode", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "one\n")
		got := NewEditLinesTool().Execute(newCtx(root), `{"path":"a.ts","mode":"append","start_line":1,"text":"x"}`).Content
		want := `unknown mode "append". Use replace, insert_after or delete.`
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_lines text required for replace", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "one\n")
		got := NewEditLinesTool().Execute(newCtx(root), `{"path":"a.ts","mode":"replace","start_line":1,"text":""}`).Content
		want := "text is required for replace. To remove lines, use mode delete."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_lines no such file", func(t *testing.T) {
		root := t.TempDir()
		got := NewEditLinesTool().Execute(newCtx(root), `{"path":"missing.ts","mode":"delete","start_line":1}`).Content
		want := "no such file: missing.ts. Use get_repo_tree or grep_code to find it, or write_file to create it."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("edit_lines success note", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "one\ntwo\n")
		got := NewEditLinesTool().Execute(newCtx(root), `{"path":"a.ts","mode":"delete","start_line":2,"end_line":2}`).Content
		want := "a.ts: deleted 1 line(s). File is now 1 lines (was 2).\n" +
			"Now reads:\n" +
			"     1→one\n" +
			"[the edit is on disk — do not read the file again to check]"
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("delete_file no such path", func(t *testing.T) {
		root := t.TempDir()
		got := NewDeleteFileTool().Execute(newCtx(root), `{"path":"missing.ts"}`).Content
		want := "no such path: missing.ts. It is already gone, or the path is wrong — check with get_repo_tree."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("delete_file needs recursive", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "pkg/a.ts", "x\n")
		got := NewDeleteFileTool().Execute(newCtx(root), `{"path":"pkg"}`).Content
		want := "pkg is a directory with 1 entries. Pass recursive:true to delete it and everything inside."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("delete_file directory success note", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "pkg/a.ts", "x\n")
		got := NewDeleteFileTool().Execute(newCtx(root), `{"path":"pkg","recursive":true}`).Content
		want := "pkg: directory deleted (1 entries). Gone from disk — do not check."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("delete_file file success note", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "xy\n")
		got := NewDeleteFileTool().Execute(newCtx(root), `{"path":"a.ts"}`).Content
		want := "a.ts: deleted (3 bytes). Gone from disk — do not check."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("move_file no such path", func(t *testing.T) {
		root := t.TempDir()
		got := NewMoveFileTool().Execute(newCtx(root), `{"from":"missing.ts","to":"b.ts"}`).Content
		want := "no such path: missing.ts. Check it with get_repo_tree."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("move_file destination exists", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "a.ts", "keep\n")
		seed(root, "b.ts", "other\n")
		got := NewMoveFileTool().Execute(newCtx(root), `{"from":"b.ts","to":"a.ts"}`).Content
		want := "a.ts already exists. Pass overwrite:true to replace it, or pick another destination."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("move_file success note", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "old/a.ts", "x\n")
		got := NewMoveFileTool().Execute(newCtx(root), `{"from":"old/a.ts","to":"new/b.ts"}`).Content
		want := "moved old/a.ts to new/b.ts. Done on disk — do not check. " +
			"Imports and references to the old path are NOT updated; grep_code for it if the file was code."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("read_file no such file", func(t *testing.T) {
		root := t.TempDir()
		got := NewReadFileTool().Execute(newCtx(root), `{"path":"missing.ts"}`).Content
		want := "no such file: missing.ts. Paths are relative to the workspace root — use get_repo_tree or grep_code to find the real path instead of guessing another one."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("read_file is a directory", func(t *testing.T) {
		root := t.TempDir()
		seed(root, "pkg/a.ts", "x\n")
		got := NewReadFileTool().Execute(newCtx(root), `{"path":"pkg"}`).Content
		want := "pkg is a directory, not a file. Use get_repo_tree to list what is inside it."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("read_file too large", func(t *testing.T) {
		root := t.TempDir()
		big := make([]byte, maxReadFileBytes+1)
		for i := range big {
			big[i] = 'a'
		}
		seed(root, "a.ts", string(big))
		got := NewReadFileTool().Execute(newCtx(root), `{"path":"a.ts"}`).Content
		want := "a.ts is 20971521 bytes, too large to read. Search it with grep_code instead."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("read_file continue hint", func(t *testing.T) {
		root := t.TempDir()
		var b []byte
		for i := 1; i <= 3; i++ {
			b = append(b, []byte("line\n")...)
		}
		seed(root, "a.ts", string(b))
		got := NewReadFileTool().Execute(newCtx(root), `{"path":"a.ts","limit":2}`).Content
		want := "a.ts — lines 1-2 of 3\n" +
			"     1→line\n" +
			"     2→line\n" +
			"\n[1 more lines. Continue with read_file offset=3.]"
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("write_file success note", func(t *testing.T) {
		root := t.TempDir()
		got := NewWriteFileTool().Execute(newCtx(root), `{"path":"a.ts","content":"x"}`).Content
		want := "a.ts: created, 1 lines, 2 bytes. The file is on disk — do not read it back to check."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})
}
