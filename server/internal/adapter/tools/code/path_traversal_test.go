package code_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// PathTraversalSuite covers the containment rule the read-only code tools have
// to hold. It matters more than it looks: the restricted "cursor" API key in
// resources/config.yml is granted grep_code and get_symbol_skeleton and denied
// run_terminal on purpose. Both tools joined a model-supplied path onto the
// workspace root with nothing checking the result, and filepath.Join Cleans
// rather than confines — so that "safe" key could read every file on the pod
// and the tool policy meant nothing.
type PathTraversalSuite struct {
	suite.Suite
	root    string
	outside string
	ctx     context.Context
	kit     *code.ToolKit
}

func TestPathTraversalSuite(t *testing.T) {
	suite.Run(t, new(PathTraversalSuite))
}

// secretMarker stands in for what actually sits outside a run's workspace on
// this host: DATABASE_URL, INTERNAL_AUTH_KEY, provider API keys.
const secretMarker = "INTERNAL_AUTH_KEY=must-never-be-read"

func (s *PathTraversalSuite) SetupTest() {
	base := s.T().TempDir()

	s.root = filepath.Join(base, "workspace")
	s.Require().NoError(os.MkdirAll(filepath.Join(s.root, "pkg"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(s.root, "pkg", "main.go"),
		[]byte("package pkg\n\n// Main is the entry point.\nfunc Main() {}\n"), 0o644))

	s.outside = filepath.Join(base, "secrets")
	s.Require().NoError(os.MkdirAll(s.outside, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(s.outside, "env.go"), []byte(
		"package secrets\n\n// "+secretMarker+"\nfunc Leak() {}\n"), 0o644))

	s.ctx = registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), uuid.New()),
		s.root,
	)
	mapperSvc := mapper.NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50})
	// No index store: get_symbol_skeleton then takes the mapper path, which is
	// the one that reads from disk.
	s.kit = code.NewToolKit(nil, nil, mapperSvc, domain.IndexerConfig{TopK: 3}, domain.GraphConfig{}, "embed-model")
}

func (s *PathTraversalSuite) tool(name string) port.ToolExecutor {
	for _, executor := range code.NewExecutors(s.kit) {
		if executor.Name() == name {
			return executor
		}
	}
	s.Require().FailNow("tool not registered: " + name)
	return nil
}

// args renders a JSON object so test cases can carry raw paths unescaped.
func args(pairs map[string]string) string {
	raw, err := json.Marshal(pairs)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func (s *PathTraversalSuite) TestGrepCodeRejectsTraversal() {
	tool := s.tool("grep_code")

	for _, path := range []string{
		"..",
		"../secrets",
		"../secrets/env.go",
		"pkg/../../secrets",
		"../../../../../../etc",
		"./../secrets",
	} {
		result := tool.Execute(s.ctx, args(map[string]string{"pattern": "INTERNAL_AUTH_KEY", "path": path}))
		s.True(result.IsError, "expected %q to be rejected, got: %s", path, result.Content)
		s.Contains(result.Content, "outside the workspace root")
		s.NotContains(result.Content, secretMarker)
	}
}

// A repository can carry a symlink out of the tree; resolving to a path that is
// textually under the root is not the same as staying inside it.
func (s *PathTraversalSuite) TestGrepCodeRejectsSymlinkEscape() {
	s.Require().NoError(os.Symlink(s.outside, filepath.Join(s.root, "escape")))
	tool := s.tool("grep_code")

	result := tool.Execute(s.ctx, args(map[string]string{"pattern": "INTERNAL_AUTH_KEY", "path": "escape"}))

	s.True(result.IsError, result.Content)
	s.Contains(result.Content, "symlink")
	s.NotContains(result.Content, secretMarker)
}

// The containment check must not cost the tool its actual job.
func (s *PathTraversalSuite) TestGrepCodeStillSearchesInsideTheWorkspace() {
	if _, err := exec.LookPath("rg"); err != nil {
		s.T().Skip("ripgrep not installed")
	}
	tool := s.tool("grep_code")

	for _, path := range []string{"", "pkg", "./pkg", "pkg/sub/.."} {
		result := tool.Execute(s.ctx, args(map[string]string{"pattern": "func Main", "path": path}))
		s.False(result.IsError, "expected %q to be accepted, got: %s", path, result.Content)
		s.Contains(result.Content, "main.go")
	}

	// A single file argument is accepted too; ripgrep omits the filename when
	// given exactly one file, so this asserts acceptance rather than a match.
	result := tool.Execute(s.ctx, args(map[string]string{"pattern": "func Main", "path": "pkg/main.go"}))
	s.False(result.IsError, result.Content)
}

func (s *PathTraversalSuite) TestGetSymbolSkeletonRejectsTraversal() {
	tool := s.tool("get_symbol_skeleton")

	for _, filePath := range []string{
		"../secrets/env.go",
		"pkg/../../secrets/env.go",
		"../../../../../../etc/passwd",
	} {
		result := tool.Execute(s.ctx, args(map[string]string{"file_path": filePath}))
		s.True(result.IsError, "expected %q to be rejected, got: %s", filePath, result.Content)
		s.Contains(result.Content, "outside the workspace root")
		s.NotContains(result.Content, secretMarker)
	}
}

func (s *PathTraversalSuite) TestGetSymbolSkeletonRejectsSymlinkEscape() {
	s.Require().NoError(os.Symlink(s.outside, filepath.Join(s.root, "escape")))
	tool := s.tool("get_symbol_skeleton")

	result := tool.Execute(s.ctx, args(map[string]string{"file_path": "escape/env.go"}))

	s.True(result.IsError, result.Content)
	s.Contains(result.Content, "symlink")
	s.NotContains(result.Content, secretMarker)
}

func (s *PathTraversalSuite) TestGetSymbolSkeletonStillReadsInsideTheWorkspace() {
	tool := s.tool("get_symbol_skeleton")

	result := tool.Execute(s.ctx, args(map[string]string{"file_path": "pkg/main.go"}))

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "Main")
}
