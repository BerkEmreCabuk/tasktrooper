package indexer_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/stretchr/testify/suite"
)

type WalkSuite struct {
	suite.Suite
}

func (s *WalkSuite) TestWalkIndexableFilesFiltersExtensions() {
	paths, err := indexer.WalkIndexableFiles(mapperFixtureRoot())
	s.Require().NoError(err)
	s.Contains(paths, "pkg/main.go")
	s.Contains(paths, "web/app.ts")
	s.Contains(paths, "scripts/run.py")
	s.NotContains(paths, "readme.md")
}

func (s *WalkSuite) TestIsIndexablePath() {
	s.True(indexer.IsIndexablePath("service.go"))
	s.True(indexer.IsIndexablePath("app.tsx"))
	s.False(indexer.IsIndexablePath("readme.md"))
}

func TestWalkSuite(t *testing.T) {
	suite.Run(t, new(WalkSuite))
}
