package indexer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/stretchr/testify/suite"
)

type IncrementalSuite struct {
	suite.Suite
	root string
}

func (s *IncrementalSuite) SetupTest() {
	s.root = s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "a.go"), []byte("package a\n"), 0644))
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "b.go"), []byte("package b\n"), 0644))
}

func (s *IncrementalSuite) TestDetectChangedFilesAddedChangedRemoved() {
	stored := map[string]string{
		"a.go": mustHash(s.root, "a.go"),
		"c.go": "oldhash",
	}
	changes := indexer.DetectChangedFiles(s.root, []string{"a.go", "b.go"}, stored)
	s.Contains(changes.Unchanged, "a.go")
	s.Contains(changes.Added, "b.go")
	s.Contains(changes.Removed, "c.go")

	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "a.go"), []byte("package changed\n"), 0644))
	changes = indexer.DetectChangedFiles(s.root, []string{"a.go", "b.go"}, stored)
	s.Contains(changes.Changed, "a.go")
}

func (s *IncrementalSuite) TestBuildFileHashes() {
	hashes, err := indexer.BuildFileHashes(s.root, []string{"a.go", "b.go"})
	s.Require().NoError(err)
	s.Len(hashes, 2)
	s.NotEmpty(hashes[0].Hash)
}

func mustHash(root, rel string) string {
	h, err := indexer.HashFile(root, rel)
	if err != nil {
		panic(err)
	}
	return h
}

func TestIncrementalSuite(t *testing.T) {
	suite.Run(t, new(IncrementalSuite))
}
