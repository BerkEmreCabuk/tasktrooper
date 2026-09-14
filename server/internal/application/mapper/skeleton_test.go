package mapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type SkeletonSuite struct {
	suite.Suite
	fixtureRoot string
}

func TestSkeletonSuite(t *testing.T) {
	suite.Run(t, new(SkeletonSuite))
}

func (s *SkeletonSuite) SetupSuite() {
	s.fixtureRoot = filepath.Join("testdata", "sample")
}

func (s *SkeletonSuite) TestExtractGoSkeleton() {
	content, err := os.ReadFile(filepath.Join(s.fixtureRoot, "pkg", "main.go"))
	s.Require().NoError(err)
	sk, err := extractGoSkeleton("pkg/main.go", content)
	s.Require().NoError(err)
	s.Equal("main", sk.Package)
	s.Contains(symbolNames(sk.Symbols, "func"), "main")
	s.Contains(symbolNames(sk.Symbols, "method"), "Greet")
	s.Contains(symbolNames(sk.Symbols, "interface"), "Greeter")
	s.Contains(symbolNames(sk.Symbols, "type"), "Hello")
	s.Contains(symbolNames(sk.Symbols, "const"), "Version")
}

func (s *SkeletonSuite) TestExtractTSSkeleton() {
	content, err := os.ReadFile(filepath.Join(s.fixtureRoot, "web", "app.ts"))
	s.Require().NoError(err)
	sk := extractTSSkeleton("web/app.ts", content)
	s.Contains(symbolNames(sk.Symbols, "function"), "greet")
	s.Contains(symbolNames(sk.Symbols, "class"), "App")
	s.Contains(symbolNames(sk.Symbols, "interface"), "Config")
	s.Contains(symbolNames(sk.Symbols, "export"), "VERSION")
}

func (s *SkeletonSuite) TestExtractPySkeleton() {
	content, err := os.ReadFile(filepath.Join(s.fixtureRoot, "scripts", "run.py"))
	s.Require().NoError(err)
	sk := extractPySkeleton("scripts/run.py", content)
	s.Equal("Sample module for mapper tests.", sk.Doc)
	s.Contains(symbolNames(sk.Symbols, "function"), "run")
	s.Contains(symbolNames(sk.Symbols, "class"), "Worker")
}

func symbolNames(symbols []Symbol, kind string) []string {
	out := make([]string, 0)
	for _, sym := range symbols {
		if sym.Kind == kind {
			out = append(out, sym.Name)
		}
	}
	return out
}
