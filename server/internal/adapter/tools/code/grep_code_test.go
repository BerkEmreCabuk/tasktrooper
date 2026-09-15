package code_test

import (
	"context"
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

// GrepCodeCaseSuite covers matching behaviour rather than containment. A model
// searching for UI copy types the casing from the ticket, and a case-sensitive
// miss reads back to it as "this string is not in the repository" — so the tool
// ignores case unless the caller explicitly asks for exact matching.
type GrepCodeCaseSuite struct {
	suite.Suite
	ctx  context.Context
	kit  *code.ToolKit
	tool port.ToolExecutor
}

func TestGrepCodeCaseSuite(t *testing.T) {
	suite.Run(t, new(GrepCodeCaseSuite))
}

func (s *GrepCodeCaseSuite) SetupTest() {
	if _, err := exec.LookPath("rg"); err != nil {
		s.T().Skip("ripgrep not installed")
	}

	root := s.T().TempDir()
	s.Require().NoError(os.WriteFile(
		filepath.Join(root, "banner.tsx"),
		[]byte("export const Banner = () => <div>Coming Soon</div>\n"), 0o644))
	s.Require().NoError(os.WriteFile(
		filepath.Join(root, ".gitignore"), []byte("vendor\n"), 0o644))
	s.Require().NoError(os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(root, "vendor", "dep.tsx"),
		[]byte("// Coming Soon, vendored\n"), 0o644))

	s.ctx = registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), uuid.New()),
		root,
	)
	mapperSvc := mapper.NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50})
	s.kit = code.NewToolKit(nil, nil, mapperSvc, domain.IndexerConfig{TopK: 3}, domain.GraphConfig{}, "embed-model")

	for _, executor := range code.NewExecutors(s.kit) {
		if executor.Name() == "grep_code" {
			s.tool = executor
		}
	}
	s.Require().NotNil(s.tool, "grep_code not registered")
}

func (s *GrepCodeCaseSuite) TestIgnoresCaseByDefault() {
	for _, pattern := range []string{"coming soon", "COMING SOON", "Coming Soon"} {
		result := s.tool.Execute(s.ctx, `{"pattern":"`+pattern+`"}`)

		s.False(result.IsError, result.Content)
		s.Contains(result.Content, "banner.tsx", "pattern %q found nothing", pattern)
	}
}

func (s *GrepCodeCaseSuite) TestGlobIgnoresCaseByDefault() {
	for _, glob := range []string{"*.tsx", "*.TSX", "*.TsX"} {
		result := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon","glob":"`+glob+`"}`)

		s.False(result.IsError, result.Content)
		s.Contains(result.Content, "banner.tsx", "glob %q filtered everything out", glob)
	}
}

// Case-insensitive globbing also loosens the "!pattern" exclusions built from
// .gitignore, so assert those still keep ignored trees out of the results.
func (s *GrepCodeCaseSuite) TestGitignoreExclusionsStillHold() {
	result := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "banner.tsx")
	s.NotContains(result.Content, "vendor")
}

func (s *GrepCodeCaseSuite) TestCaseSensitiveOptIn() {
	exact := s.tool.Execute(s.ctx, `{"pattern":"Coming Soon","case_sensitive":true}`)
	s.False(exact.IsError, exact.Content)
	s.Contains(exact.Content, "banner.tsx")

	wrongCase := s.tool.Execute(s.ctx, `{"pattern":"coming soon","case_sensitive":true}`)
	s.False(wrongCase.IsError, wrongCase.Content)
	s.NotContains(wrongCase.Content, "banner.tsx")
}
