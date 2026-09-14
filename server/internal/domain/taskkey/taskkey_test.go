package taskkey_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain/taskkey"
	"github.com/stretchr/testify/suite"
)

type TeamKeySuite struct {
	suite.Suite
}

func TestTeamKeySuite(t *testing.T) {
	suite.Run(t, new(TeamKeySuite))
}

func (s *TeamKeySuite) TestSuggestKeyPrefix() {
	s.Equal("DEVTE", taskkey.SuggestKeyPrefix("Dev Team"))
	s.Equal("TM", taskkey.SuggestKeyPrefix(""))
}

func (s *TeamKeySuite) TestValidateKeyPrefix() {
	s.NoError(taskkey.ValidateKeyPrefix("DT"))
	s.Error(taskkey.ValidateKeyPrefix("d"))
	s.Error(taskkey.ValidateKeyPrefix("TOOLONG"))
}

func (s *TeamKeySuite) TestFormatTaskKey() {
	s.Equal("D-1", taskkey.FormatTaskKey("d", 1))
}
