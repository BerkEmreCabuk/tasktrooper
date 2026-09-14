package chunker_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
)

type ChunkerSuite struct {
	suite.Suite
}

func (s *ChunkerSuite) TestGoChunkerExtractsFunctionAndType() {
	src := []byte(`package sample

type Config struct {
	Timeout int
}

func Hello(name string) string {
	return "hello " + name
}
`)
	chunks, err := chunker.GoChunker{}.Chunk("service.go", src)
	s.Require().NoError(err)
	s.Require().Len(chunks, 2)

	s.Equal("type", chunks[0].Kind)
	s.Equal("Config", chunks[0].SymbolName)
	s.Equal("go", chunks[0].Language)

	s.Equal("function", chunks[1].Kind)
	s.Equal("Hello", chunks[1].SymbolName)
	s.Contains(chunks[1].Content, `return "hello " + name`)
	s.Contains(chunks[1].Signature, "func Hello")
}

func (s *ChunkerSuite) TestGoChunkerSplitsLargeFunction() {
	var bodyLines string
	for i := 0; i < 160; i++ {
		bodyLines += "\n\t_ = 1"
	}
	src := []byte("package sample\n\nfunc Big() {\n\tif true {\n\t\tx := 1" + bodyLines + "\n\t}\n}\n")

	chunks, err := chunker.GoChunker{}.Chunk("big.go", src)
	s.Require().NoError(err)
	s.Require().NotEmpty(chunks)

	foundBlock := false
	for _, ch := range chunks {
		if ch.Kind == "block" {
			foundBlock = true
			s.Equal("Big.block1", ch.SymbolName)
		}
	}
	s.True(foundBlock)
}

func (s *ChunkerSuite) TestFallbackChunkerOverlap() {
	lines := make([]byte, 0, 250*6)
	for i := 1; i <= 250; i++ {
		if i > 1 {
			lines = append(lines, '\n')
		}
		lines = append(lines, []byte("line content")...)
	}

	chunks, err := chunker.FallbackChunker{}.Chunk("notes.txt", lines)
	s.Require().NoError(err)
	s.Require().Len(chunks, 2)
	s.Equal(1, chunks[0].StartLine)
	s.Equal(200, chunks[0].EndLine)
	s.Equal(181, chunks[1].StartLine)
	s.Equal(250, chunks[1].EndLine)
}

func (s *ChunkerSuite) TestRegistryUsesGoAndFallback() {
	reg := chunker.DefaultRegistry()

	goSrc := []byte("package main\n\nfunc main() {}\n")
	goChunks, err := reg.Chunk("main.go", goSrc)
	s.Require().NoError(err)
	s.Require().Len(goChunks, 1)
	s.Equal("function", goChunks[0].Kind)

	txt := []byte("alpha\nbeta\n")
	txtChunks, err := reg.Chunk("readme.md", txt)
	s.Require().NoError(err)
	s.Require().Len(txtChunks, 1)
	s.Equal("file", txtChunks[0].Kind)
}

func TestChunkerSuite(t *testing.T) {
	suite.Run(t, new(ChunkerSuite))
}
