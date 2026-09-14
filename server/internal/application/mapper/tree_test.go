package mapper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type TreeSuite struct {
	suite.Suite
	paths []string
}

func TestTreeSuite(t *testing.T) {
	suite.Run(t, new(TreeSuite))
}

func (s *TreeSuite) SetupSuite() {
	s.paths = []string{
		"pkg/main.go",
		"web/app.ts",
		"scripts/run.py",
		"readme.md",
	}
}

func (s *TreeSuite) TestBuildTreeContainsEntries() {
	out := BuildTree(s.paths, 4, 10)
	s.Contains(out, "pkg/")
	s.Contains(out, "main.go")
	s.Contains(out, "web/")
	s.Contains(out, "app.ts")
}

func (s *TreeSuite) TestBuildTreeMaxDepth() {
	out := BuildTree(s.paths, 1, 10)
	s.Contains(out, "pkg/")
	s.NotContains(out, "main.go")
}

func (s *TreeSuite) TestBuildTreeMaxFiles() {
	many := []string{
		"files/filea.txt",
		"files/fileb.txt",
		"files/filec.txt",
		"files/filed.txt",
		"files/filee.txt",
	}
	out := BuildTree(many, 4, 2)
	s.True(strings.Contains(out, "more files"))
}

func (s *TreeSuite) TestExpandTreePrefix() {
	out := ExpandTree("pkg", s.paths, 4)
	s.Contains(out, "pkg")
	s.Contains(out, "main.go")
	s.NotContains(out, "app.ts")
}

func (s *TreeSuite) TestBuildTreeEmpty() {
	out := BuildTree(nil, 4, 10)
	s.Equal(".\n", out)
}
