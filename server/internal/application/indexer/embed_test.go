package indexer_test

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/stretchr/testify/suite"
)

type EmbedSuite struct {
	suite.Suite
}

func (s *EmbedSuite) TestFormatEmbedInput() {
	got := indexer.FormatEmbedInput("auth.go", "Encrypt", "func Encrypt(string) (string, error)", "return hash")
	s.True(strings.HasPrefix(got, "auth.go Encrypt func Encrypt(string) (string, error)\n"))
	s.Contains(got, "return hash")
}

func TestEmbedSuite(t *testing.T) {
	suite.Run(t, new(EmbedSuite))
}
