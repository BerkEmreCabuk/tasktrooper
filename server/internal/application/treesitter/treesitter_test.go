package treesitter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

type TreeSitterSuite struct {
	suite.Suite
	fixtureRoot string
}

func TestTreeSitterSuite(t *testing.T) {
	suite.Run(t, new(TreeSitterSuite))
}

func (s *TreeSitterSuite) SetupSuite() {
	s.fixtureRoot = filepath.Join("..", "mapper", "testdata", "sample")
}

func (s *TreeSitterSuite) TestExtractTSSkeleton() {
	content, err := os.ReadFile(filepath.Join(s.fixtureRoot, "web", "app.ts"))
	s.Require().NoError(err)
	symbols := treesitter.ExtractTSSkeleton(content, "app.ts")
	names := symbolNames(symbols, "function")
	s.Contains(names, "greet")
	s.Contains(symbolNames(symbols, "class"), "App")
	s.Contains(symbolNames(symbols, "interface"), "Config")
	s.Contains(symbolNames(symbols, "export"), "VERSION")
}

func (s *TreeSitterSuite) TestExtractPySkeleton() {
	content, err := os.ReadFile(filepath.Join(s.fixtureRoot, "scripts", "run.py"))
	s.Require().NoError(err)
	doc, symbols := treesitter.ExtractPySkeleton(content)
	s.Equal("Sample module for mapper tests.", doc)
	s.Contains(symbolNamesPy(symbols, "function"), "run")
	s.Contains(symbolNamesPy(symbols, "class"), "Worker")
}

func (s *TreeSitterSuite) TestExtractCallsInRange() {
	src := []byte(`export function run() {
  helper();
  service.process();
}

export function other() {
  skipped();
}
`)
	calls := treesitter.ExtractCallsInRange("run.ts", src, 1, 4)
	names := make(map[string]struct{})
	for _, call := range calls {
		names[call.Name] = struct{}{}
	}
	s.Contains(names, "helper")
	s.Contains(names, "process")
	s.NotContains(names, "skipped")
}

func (s *TreeSitterSuite) TestExtractTSImports() {
	src := []byte(`import { readFile } from 'fs/promises';
const path = require('./util');
`)
	paths := treesitter.ExtractImportPaths("imports.ts", src)
	s.Require().Len(paths, 2)
	s.Equal("fs/promises", paths[0])
	s.Equal("./util", paths[1])
}

func symbolNames(symbols []treesitter.TSSymbol, kind string) []string {
	out := make([]string, 0)
	for _, sym := range symbols {
		if sym.Kind == kind {
			out = append(out, sym.Name)
		}
	}
	return out
}

func symbolNamesPy(symbols []treesitter.PySymbol, kind string) []string {
	out := make([]string, 0)
	for _, sym := range symbols {
		if sym.Kind == kind {
			out = append(out, sym.Name)
		}
	}
	return out
}
