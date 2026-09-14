package code_test

import (
	"context"
	"fmt"
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

// ReadFileSuite covers the tool that exists so agents stop paging through
// source with `sed -n '630,640p'`: one call has to hand back enough of the file
// to work with, and say where what it returned sits in the whole.
type ReadFileSuite struct {
	suite.Suite
	ctx  context.Context
	root string
	tool port.ToolExecutor
}

func TestReadFileSuite(t *testing.T) {
	suite.Run(t, new(ReadFileSuite))
}

func (s *ReadFileSuite) SetupTest() {
	s.root = s.T().TempDir()
	s.ctx = registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), uuid.New()),
		s.root,
	)
	s.tool = code.NewReadFileTool()
}

func (s *ReadFileSuite) write(name, content string) {
	path := filepath.Join(s.root, name)
	s.Require().NoError(os.MkdirAll(filepath.Dir(path), 0o755))
	s.Require().NoError(os.WriteFile(path, []byte(content), 0o644))
}

func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func (s *ReadFileSuite) TestReadsWholeFileWithLineNumbers() {
	s.write("app/route.ts", lines(12))

	result := s.tool.Execute(s.ctx, `{"path":"app/route.ts"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "lines 1-12 of 12")
	s.Contains(result.Content, "     1→line 1")
	s.Contains(result.Content, "    12→line 12")
	s.Contains(result.Content, "[end of file]")
}

// The whole point of the tool: a file far longer than any window an agent would
// have sed'd through comes back in ONE call, and the model is told how much is
// left rather than having to probe for the end.
func (s *ReadFileSuite) TestLongFileReturnsOneLargeWindowAndSaysWhatRemains() {
	s.write("big.ts", lines(2000))

	result := s.tool.Execute(s.ctx, `{"path":"big.ts"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "of 2000")
	s.Contains(result.Content, "Continue with read_file offset=")
	s.NotContains(result.Content, "[end of file]")
	// Comfortably more than the ten lines a shell window was giving it.
	s.Greater(strings.Count(result.Content, "→"), 300)
}

func (s *ReadFileSuite) TestOffsetContinuesFromTheGivenLine() {
	s.write("app.ts", lines(50))

	result := s.tool.Execute(s.ctx, `{"path":"app.ts","offset":40}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "lines 40-50 of 50")
	s.Contains(result.Content, "    40→line 40")
	s.NotContains(result.Content, "    39→line 39")
}

func (s *ReadFileSuite) TestOffsetPastEndSaysSoInsteadOfReturningNothing() {
	s.write("app.ts", lines(5))

	result := s.tool.Execute(s.ctx, `{"path":"app.ts","offset":900}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "has 5 lines")
}

func (s *ReadFileSuite) TestMissingFileNamesTheFixInsteadOfGuessing() {
	result := s.tool.Execute(s.ctx, `{"path":"nope.ts"}`)

	s.True(result.IsError)
	s.Contains(result.Content, "no such file")
	s.Contains(result.Content, "get_repo_tree")
}

func (s *ReadFileSuite) TestDirectoryIsRefused() {
	s.write("pkg/app.ts", "x\n")

	result := s.tool.Execute(s.ctx, `{"path":"pkg"}`)

	s.True(result.IsError)
	s.Contains(result.Content, "directory")
}

// Same containment rule grep_code has: `path` is model output, and a Join
// without a check turns "../.." into a readable path outside the workspace.
func (s *ReadFileSuite) TestPathEscapingTheWorkspaceIsRefused() {
	outside := filepath.Join(s.T().TempDir(), "secret.env")
	s.Require().NoError(os.WriteFile(outside, []byte("TOKEN=abc\n"), 0o600))

	result := s.tool.Execute(s.ctx, fmt.Sprintf(`{"path":%q}`, "../"+filepath.Base(filepath.Dir(outside))+"/secret.env"))

	s.True(result.IsError)
	s.NotContains(result.Content, "TOKEN=abc")
}

func (s *ReadFileSuite) TestBinaryFileIsRefusedRatherThanRendered() {
	s.write("bin.dat", string([]byte{0xff, 0xfe, 0x00, 0x01}))

	result := s.tool.Execute(s.ctx, `{"path":"bin.dat"}`)

	s.True(result.IsError)
	s.Contains(result.Content, "not a text file")
}

// A minified bundle is one line of hundreds of thousands of characters. The
// character cap has to win over the line cap, or the loop cuts the middle out
// of the result and the model believes it read what was removed.
func (s *ReadFileSuite) TestOneEnormousLineIsClippedNotDropped() {
	s.write("bundle.js", strings.Repeat("a", 200000)+"\n")

	result := s.tool.Execute(s.ctx, `{"path":"bundle.js"}`)

	s.False(result.IsError, result.Content)
	s.Less(len(result.Content), 16000)
	s.Contains(result.Content, "     1→")
}

func (s *ReadFileSuite) TestPathIsRequired() {
	result := s.tool.Execute(s.ctx, `{}`)

	s.True(result.IsError)
	s.Contains(result.Content, "path is required")
}
