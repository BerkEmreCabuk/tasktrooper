package paths_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/paths"
)

type PathsSuite struct {
	suite.Suite
	originalHome string
	originalXDG  string
	tempHome     string
}

func (s *PathsSuite) SetupTest() {
	s.originalHome = os.Getenv("HOME")
	s.originalXDG = os.Getenv("XDG_DATA_HOME")
	s.tempHome = s.T().TempDir()
	s.Require().NoError(os.Setenv("HOME", s.tempHome))
	s.Require().NoError(os.Setenv("XDG_DATA_HOME", ""))
}

func (s *PathsSuite) TearDownTest() {
	s.Require().NoError(os.Setenv("HOME", s.originalHome))
	s.Require().NoError(os.Setenv("XDG_DATA_HOME", s.originalXDG))
}

func (s *PathsSuite) TestResolveCreatesLayout() {
	layout, err := paths.Resolve()
	s.Require().NoError(err)
	s.Require().NotNil(layout)

	switch runtime.GOOS {
	case "darwin":
		expected := filepath.Join(s.tempHome, "Library", "Application Support", "local-llm")
		s.Equal(expected, layout.Root)
	case "linux":
		expected := filepath.Join(s.tempHome, ".local", "share", "local-llm")
		s.Equal(expected, layout.Root)
	default:
		s.T().Skip("platform-specific test")
	}

	for _, dir := range []string{layout.Root, layout.Workspaces, layout.Files, layout.Postgres} {
		info, err := os.Stat(dir)
		s.Require().NoError(err)
		s.True(info.IsDir())
	}

	s.Equal(filepath.Join(layout.Root, "config.yml"), layout.Config)
}

func (s *PathsSuite) TestResolveUsesXDGDataHomeOnLinux() {
	if runtime.GOOS != "linux" {
		s.T().Skip("linux-only test")
	}
	xdg := filepath.Join(s.tempHome, "xdg-data")
	s.Require().NoError(os.Setenv("XDG_DATA_HOME", xdg))

	layout, err := paths.Resolve()
	s.Require().NoError(err)
	s.Equal(filepath.Join(xdg, "local-llm"), layout.Root)
}

func TestPathsSuite(t *testing.T) {
	suite.Run(t, new(PathsSuite))
}
