package code_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// FileToolsSuite covers the tools that replaced `sed -i`, `rm`, `mv` and shell
// heredocs. The behaviour that matters beyond "the bytes changed" is what each
// result SAYS: the count and the line numbers are what let a run skip the
// verification grep it used to spend a second turn on.
type FileToolsSuite struct {
	suite.Suite
	ctx    context.Context
	root   string
	write  port.ToolExecutor
	edit   port.ToolExecutor
	lines  port.ToolExecutor
	del    port.ToolExecutor
	move   port.ToolExecutor
	reader port.ToolExecutor
}

func TestFileToolsSuite(t *testing.T) {
	suite.Run(t, new(FileToolsSuite))
}

func (s *FileToolsSuite) SetupTest() {
	s.root = s.T().TempDir()
	s.ctx = registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), uuid.New()),
		s.root,
	)
	s.write = code.NewWriteFileTool()
	s.edit = code.NewEditFileTool()
	s.lines = code.NewEditLinesTool()
	s.del = code.NewDeleteFileTool()
	s.move = code.NewMoveFileTool()
	s.reader = code.NewReadFileTool()
}

func (s *FileToolsSuite) seed(name, content string) {
	path := filepath.Join(s.root, name)
	s.Require().NoError(os.MkdirAll(filepath.Dir(path), 0o755))
	s.Require().NoError(os.WriteFile(path, []byte(content), 0o644))
}

func (s *FileToolsSuite) read(name string) string {
	raw, err := os.ReadFile(filepath.Join(s.root, name))
	s.Require().NoError(err)
	return string(raw)
}

// --- write_file ---

func (s *FileToolsSuite) TestWriteCreatesFileAndParentDirectories() {
	result := s.write.Execute(s.ctx, `{"path":"src/app/new.ts","content":"export const a = `+"`x`"+`;"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "created")
	// The backtick is the whole point: a shell heredoc could not carry it.
	s.Equal("export const a = `x`;\n", s.read("src/app/new.ts"))
}

func (s *FileToolsSuite) TestWriteOverwritesAndSaysSo() {
	s.seed("a.ts", "old\n")

	result := s.write.Execute(s.ctx, `{"path":"a.ts","content":"new"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "overwritten")
	s.Equal("new\n", s.read("a.ts"))
}

func (s *FileToolsSuite) TestWriteRefusesToEscapeTheWorkspace() {
	result := s.write.Execute(s.ctx, `{"path":"../escaped.ts","content":"x"}`)

	s.True(result.IsError)
	s.NoFileExists(filepath.Join(filepath.Dir(s.root), "escaped.ts"))
}

// --- edit_file ---

func (s *FileToolsSuite) TestEditReportsCountAndLinesSoNoGrepIsNeeded() {
	s.seed("a.ts", "const androidSoon = 1\nconst b = 2\nuse(androidSoon)\n")

	result := s.edit.Execute(s.ctx, `{"path":"a.ts","old_string":"androidSoon","new_string":"androidLink","replace_all":true}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "2 occurrences replaced")
	s.Contains(result.Content, "line 1, 3")
	s.Contains(result.Content, "do not grep")
	s.NotContains(s.read("a.ts"), "androidSoon")
}

// Ambiguity is refused rather than guessed at: replacing the first of four
// matches silently is how an agent "fixes" one call site and leaves three.
func (s *FileToolsSuite) TestEditRefusesAnAmbiguousMatchAndNamesTheWayOut() {
	s.seed("a.ts", "x\nx\nx\n")

	result := s.edit.Execute(s.ctx, `{"path":"a.ts","old_string":"x","new_string":"y"}`)

	s.True(result.IsError)
	s.Contains(result.Content, "matches 3 places")
	s.Contains(result.Content, "replace_all")
	s.Equal("x\nx\nx\n", s.read("a.ts"))
}

func (s *FileToolsSuite) TestEditSaysHowToFixAStringItCouldNotFind() {
	s.seed("a.ts", "const a = 1\n")

	result := s.edit.Execute(s.ctx, `{"path":"a.ts","old_string":"     1→const a = 1","new_string":"const a = 2"}`)

	s.True(result.IsError)
	s.Contains(result.Content, "line-number prefix")
}

func (s *FileToolsSuite) TestEditDeletesWhenNewStringIsEmpty() {
	s.seed("a.ts", "keep\nDROP\nkeep\n")

	result := s.edit.Execute(s.ctx, `{"path":"a.ts","old_string":"DROP\n","new_string":""}`)

	s.False(result.IsError, result.Content)
	s.Equal("keep\nkeep\n", s.read("a.ts"))
}

func (s *FileToolsSuite) TestEditPreservesTheExecutableBit() {
	path := filepath.Join(s.root, "run.sh")
	s.Require().NoError(os.WriteFile(path, []byte("echo old\n"), 0o755))

	result := s.edit.Execute(s.ctx, `{"path":"run.sh","old_string":"old","new_string":"new"}`)

	s.False(result.IsError, result.Content)
	info, err := os.Stat(path)
	s.Require().NoError(err)
	s.Equal(os.FileMode(0o755), info.Mode().Perm())
}

// --- edit_lines ---

func (s *FileToolsSuite) TestInsertAfterAddsLinesAndShowsTheResult() {
	s.seed("a.ts", "import a\n\nfunc main() {}\n")

	result := s.lines.Execute(s.ctx, `{"path":"a.ts","mode":"insert_after","start_line":1,"text":"import b"}`)

	s.False(result.IsError, result.Content)
	s.Equal("import a\nimport b\n\nfunc main() {}\n", s.read("a.ts"))
	// The rendered window is what removes the follow-up read.
	s.Contains(result.Content, "Now reads:")
	s.Contains(result.Content, "import b")
	s.Contains(result.Content, "4 lines (was 3)")
}

func (s *FileToolsSuite) TestInsertAtTopOfFile() {
	s.seed("a.ts", "second\n")

	result := s.lines.Execute(s.ctx, `{"path":"a.ts","mode":"insert_after","start_line":0,"text":"first"}`)

	s.False(result.IsError, result.Content)
	s.Equal("first\nsecond\n", s.read("a.ts"))
}

func (s *FileToolsSuite) TestReplaceRangeWithMoreLinesThanItHad() {
	s.seed("a.ts", "one\nTWO\nthree\n")

	result := s.lines.Execute(s.ctx, `{"path":"a.ts","mode":"replace","start_line":2,"end_line":2,"text":"a\nb"}`)

	s.False(result.IsError, result.Content)
	s.Equal("one\na\nb\nthree\n", s.read("a.ts"))
}

func (s *FileToolsSuite) TestDeleteRangeRemovesTheLines() {
	s.seed("a.ts", "one\ntwo\nthree\nfour\n")

	result := s.lines.Execute(s.ctx, `{"path":"a.ts","mode":"delete","start_line":2,"end_line":3}`)

	s.False(result.IsError, result.Content)
	s.Equal("one\nfour\n", s.read("a.ts"))
	s.Contains(result.Content, "deleted 2 line(s)")
}

func (s *FileToolsSuite) TestLineRangePastTheEndIsRefusedWithTheRealLength() {
	s.seed("a.ts", "one\ntwo\n")

	result := s.lines.Execute(s.ctx, `{"path":"a.ts","mode":"delete","start_line":9,"end_line":12}`)

	s.True(result.IsError)
	s.Contains(result.Content, "2 lines")
	s.Equal("one\ntwo\n", s.read("a.ts"))
}

func (s *FileToolsSuite) TestUnknownModeIsRefused() {
	s.seed("a.ts", "one\n")

	result := s.lines.Execute(s.ctx, `{"path":"a.ts","mode":"append","start_line":1,"text":"x"}`)

	s.True(result.IsError)
	s.Contains(result.Content, "insert_after")
}

// read_file numbers the lines edit_lines takes back, so the two have to agree
// on what line 1 is.
func (s *FileToolsSuite) TestLineNumbersMatchWhatReadFileShows() {
	s.seed("a.ts", "alpha\nbeta\ngamma\n")

	read := s.reader.Execute(s.ctx, `{"path":"a.ts"}`)
	s.Require().False(read.IsError, read.Content)
	s.Contains(read.Content, "     2→beta")

	result := s.lines.Execute(s.ctx, `{"path":"a.ts","mode":"replace","start_line":2,"text":"BETA"}`)

	s.False(result.IsError, result.Content)
	s.Equal("alpha\nBETA\ngamma\n", s.read("a.ts"))
}

// --- delete_file / move_file ---

func (s *FileToolsSuite) TestDeleteRemovesAFile() {
	s.seed("a.ts", "x\n")

	result := s.del.Execute(s.ctx, `{"path":"a.ts"}`)

	s.False(result.IsError, result.Content)
	s.NoFileExists(filepath.Join(s.root, "a.ts"))
}

func (s *FileToolsSuite) TestDeleteNeedsRecursiveForANonEmptyDirectory() {
	s.seed("pkg/a.ts", "x\n")

	result := s.del.Execute(s.ctx, `{"path":"pkg"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "recursive")
	s.FileExists(filepath.Join(s.root, "pkg", "a.ts"))

	result = s.del.Execute(s.ctx, `{"path":"pkg","recursive":true}`)
	s.False(result.IsError, result.Content)
	s.NoDirExists(filepath.Join(s.root, "pkg"))
}

// .git IS the workspace: deleting it destroys the branch, the history and the
// run's only way to hand the work back.
func (s *FileToolsSuite) TestDeleteRefusesToTouchGit() {
	s.seed(".git/config", "[core]\n")

	result := s.del.Execute(s.ctx, `{"path":".git","recursive":true}`)

	s.True(result.IsError)
	s.Contains(result.Content, "protected")
	s.FileExists(filepath.Join(s.root, ".git", "config"))
}

func (s *FileToolsSuite) TestDeleteRefusesTheWorkspaceRoot() {
	result := s.del.Execute(s.ctx, `{"path":".","recursive":true}`)

	s.True(result.IsError)
	s.Contains(result.Content, "workspace root")
	s.DirExists(s.root)
}

func (s *FileToolsSuite) TestMoveRenamesAndCreatesMissingParents() {
	s.seed("old/a.ts", "x\n")

	result := s.move.Execute(s.ctx, `{"from":"old/a.ts","to":"new/nested/b.ts"}`)

	s.False(result.IsError, result.Content)
	s.NoFileExists(filepath.Join(s.root, "old", "a.ts"))
	s.Equal("x\n", s.read("new/nested/b.ts"))
	// A rename that leaves imports pointing at the old path is the classic
	// half-done refactor, so the result says so.
	s.Contains(result.Content, "NOT updated")
}

func (s *FileToolsSuite) TestMoveWillNotClobberWithoutOverwrite() {
	s.seed("a.ts", "keep\n")
	s.seed("b.ts", "other\n")

	result := s.move.Execute(s.ctx, `{"from":"b.ts","to":"a.ts"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "already exists")
	s.Equal("keep\n", s.read("a.ts"))

	result = s.move.Execute(s.ctx, `{"from":"b.ts","to":"a.ts","overwrite":true}`)
	s.False(result.IsError, result.Content)
	s.Equal("other\n", s.read("a.ts"))
}

func (s *FileToolsSuite) TestMoveRefusesToEscapeTheWorkspace() {
	s.seed("a.ts", "x\n")

	result := s.move.Execute(s.ctx, `{"from":"a.ts","to":"../escaped.ts"}`)

	s.True(result.IsError)
	s.FileExists(filepath.Join(s.root, "a.ts"))
	s.NoFileExists(filepath.Join(filepath.Dir(s.root), "escaped.ts"))
}

func (s *FileToolsSuite) TestEveryWriteToolRefusesAMissingPath() {
	for name, tool := range map[string]port.ToolExecutor{
		"write_file": s.write, "edit_file": s.edit, "edit_lines": s.lines, "delete_file": s.del,
	} {
		result := tool.Execute(s.ctx, `{}`)
		s.True(result.IsError, name)
		s.True(strings.Contains(result.Content, "required"), "%s: %s", name, result.Content)
	}
}
