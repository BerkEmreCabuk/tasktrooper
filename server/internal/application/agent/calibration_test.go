package agent_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
)

type TokenCalibrationSuite struct {
	suite.Suite
}

func TestTokenCalibrationSuite(t *testing.T) {
	suite.Run(t, new(TokenCalibrationSuite))
}

func (s *TokenCalibrationSuite) TestStartsAtTheInitialRatio() {
	c := agent.NewTokenCalibrationForTest()
	s.Equal(agent.CalibrationInitialRatioForTest(), c.Current())
}

func (s *TokenCalibrationSuite) TestConvergesTowardAConsistentRealRatio() {
	c := agent.NewTokenCalibrationForTest()
	for range 20 {
		c.Observe(2000, 1000)
	}
	s.InDelta(2.0, c.Current(), 0.01, "ratio must converge close to the true 2x factor")
}

func (s *TokenCalibrationSuite) TestOneSampleMovesTowardButNotOntoItself() {
	c := agent.NewTokenCalibrationForTest()
	before := c.Current()
	c.Observe(3000, 1000) // sample ratio 3.0
	after := c.Current()
	s.Greater(after, before, "a higher-than-current sample must raise the ratio")
	s.Less(after, 3.0, "a single sample must not jump the ratio all the way to it")
}

func (s *TokenCalibrationSuite) TestSampleAboveTheCeilingIsIgnored() {
	c := agent.NewTokenCalibrationForTest()
	s.Require().Less(agent.MaxCalibrationRatioForTest(), 10.0)
	c.Observe(10000, 1000) // sample ratio 10.0
	s.Equal(agent.CalibrationInitialRatioForTest(), c.Current(), "an outlier above the ceiling must not move the ratio at all")
}

func (s *TokenCalibrationSuite) TestSampleBelowTheFloorIsIgnored() {
	c := agent.NewTokenCalibrationForTest()
	s.Require().Greater(agent.MinCalibrationRatioForTest(), 0.01)
	c.Observe(10, 1000) // sample ratio 0.01
	s.Equal(agent.CalibrationInitialRatioForTest(), c.Current())
}

func (s *TokenCalibrationSuite) TestSampleAtTheBoundaryIsAccepted() {
	c := agent.NewTokenCalibrationForTest()
	before := c.Current()
	c.Observe(400, 1000) // sample ratio 0.4, inside [0.25, 4]
	s.NotEqual(before, c.Current())
}

func (s *TokenCalibrationSuite) TestZeroRealIsSafeAndIgnored() {
	c := agent.NewTokenCalibrationForTest()
	s.NotPanics(func() { c.Observe(0, 1000) })
	s.Equal(agent.CalibrationInitialRatioForTest(), c.Current())

	s.NotPanics(func() { c.Observe(-100, 1000) })
	s.Equal(agent.CalibrationInitialRatioForTest(), c.Current())
}

func (s *TokenCalibrationSuite) TestZeroOrNegativeEstimatedIsSafeAndIgnored() {
	c := agent.NewTokenCalibrationForTest()
	s.NotPanics(func() { c.Observe(1000, 0) })
	s.Equal(agent.CalibrationInitialRatioForTest(), c.Current())

	s.NotPanics(func() { c.Observe(1000, -5) })
	s.Equal(agent.CalibrationInitialRatioForTest(), c.Current())
}
