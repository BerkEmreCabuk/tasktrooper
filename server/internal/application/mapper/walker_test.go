package mapper

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type WalkerSuite struct {
	suite.Suite
	fixtureRoot string
}

func TestWalkerSuite(t *testing.T) {
	suite.Run(t, new(WalkerSuite))
}

func (s *WalkerSuite) SetupSuite() {
	s.fixtureRoot = filepath.Join("testdata", "sample")
}

func (s *WalkerSuite) TestWalkDefaultIgnoresGitignore() {
	paths, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: true})
	s.Require().NoError(err)
	s.Contains(paths, "pkg/main.go")
	s.Contains(paths, "web/app.ts")
	s.Contains(paths, "scripts/run.py")
	s.Contains(paths, "readme.md")
	s.NotContains(paths, "debug.log")
	s.NotContains(paths, "ignored/secret.txt")
}

func (s *WalkerSuite) TestWalkWithoutGitignoreIncludesLog() {
	paths, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: false})
	s.Require().NoError(err)
	s.Contains(paths, "debug.log")
}

func (s *WalkerSuite) TestWalkMissingRoot() {
	_, err := Walk(filepath.Join("testdata", "missing"), WalkOptions{})
	s.Error(err)
}
