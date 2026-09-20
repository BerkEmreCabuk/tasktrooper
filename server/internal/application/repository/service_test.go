package repository_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type BoardTaskDomainSuite struct {
	suite.Suite
}

func TestBoardTaskDomainSuite(t *testing.T) {
	suite.Run(t, new(BoardTaskDomainSuite))
}

func (s *BoardTaskDomainSuite) TestValidTaskPriority() {
	s.True(domain.ValidTaskPriority(domain.TaskPriorityMedium))
	s.False(domain.ValidTaskPriority("urgent"))
}
