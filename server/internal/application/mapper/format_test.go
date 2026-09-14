package mapper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type FormatSuite struct {
	suite.Suite
}

func TestFormatSuite(t *testing.T) {
	suite.Run(t, new(FormatSuite))
}

func (s *FormatSuite) TestFormatSkeleton() {
	out := FormatSkeleton([]Symbol{
		{Kind: "func", Name: "Run", Doc: "runs the job"},
		{Kind: "type", Name: "Config"},
	}, 200)
	s.Contains(out, "func Run — runs the job")
	s.Contains(out, "type Config")
}

func (s *FormatSuite) TestFormatSkeletonTruncatesDoc() {
	longDoc := strings.Repeat("word ", 100)
	out := FormatSkeleton([]Symbol{
		{Kind: "func", Name: "Run", Doc: longDoc},
	}, 20)
	s.Contains(out, "...")
	s.Less(len(out), 60)
}

func (s *FormatSuite) TestFormatFileSkeleton() {
	out := FormatFileSkeleton(FileSkeleton{
		Path:    "pkg/main.go",
		Package: "main",
		Imports: []string{"fmt"},
		Symbols: []Symbol{
			{Kind: "func", Name: "main"},
		},
	}, 200)
	s.Contains(out, "pkg/main.go (package main)")
	s.Contains(out, "imports: fmt")
	s.Contains(out, "func main")
}
