package mapper

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type ServiceSuite struct {
	suite.Suite
	fixtureRoot string
	svc         *Service
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupSuite() {
	s.fixtureRoot = filepath.Join("testdata", "sample")
	s.svc = NewService(domain.MappingConfig{
		Enabled:           true,
		TreeMaxDepth:      4,
		MaxFiles:          50,
		SkeletonMaxTokens: 100,
	})
}

func (s *ServiceSuite) TestBuildTree() {
	out, err := s.svc.BuildTree(s.fixtureRoot)
	s.Require().NoError(err)
	s.Contains(out, "pkg/")
	s.Contains(out, "main.go")
}

func (s *ServiceSuite) TestBuildSkeleton() {
	out, err := s.svc.BuildSkeleton(s.fixtureRoot)
	s.Require().NoError(err)
	s.Contains(out, "pkg/main.go")
	s.Contains(out, "func main")
	s.Contains(out, "web/app.ts")
	s.Contains(out, "scripts/run.py")
	s.LessOrEqual(len(out), s.svc.skeletonCharLimit())
}

func (s *ServiceSuite) TestBuildPackageSummary() {
	out, err := s.svc.BuildPackageSummary(s.fixtureRoot)
	s.Require().NoError(err)
	s.Contains(out, "# Repository Structure")
	s.Contains(out, "# Code Skeleton")
	s.Contains(out, "main.go")
	s.LessOrEqual(len(out), s.svc.skeletonCharLimit())
}

func (s *ServiceSuite) TestDisabledService() {
	disabled := NewService(domain.MappingConfig{Enabled: false})
	tree, err := disabled.BuildTree(s.fixtureRoot)
	s.Require().NoError(err)
	s.Empty(tree)
	sk, err := disabled.BuildSkeleton(s.fixtureRoot)
	s.Require().NoError(err)
	s.Empty(sk)
}

func (s *ServiceSuite) TestSkeletonRespectsCharLimit() {
	tight := NewService(domain.MappingConfig{
		Enabled:           true,
		SkeletonMaxTokens: 10,
	})
	out, err := tight.BuildSkeleton(s.fixtureRoot)
	s.Require().NoError(err)
	s.LessOrEqual(len(out), 40)
	s.True(len(out) > 0 || strings.TrimSpace(out) == "")
}
