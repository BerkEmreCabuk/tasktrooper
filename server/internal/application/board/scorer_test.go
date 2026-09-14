package board_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type recordedDelta struct {
	agentID   uuid.UUID
	eventType string
	delta     float64
}

type fakePerfStore struct{ applied []recordedDelta }

func (f *fakePerfStore) ApplyDelta(_ context.Context, in domain.ApplyScoreInput) (domain.AgentPerformanceScore, error) {
	f.applied = append(f.applied, recordedDelta{in.AgentID, in.EventType, in.Delta})
	return domain.AgentPerformanceScore{}, nil
}

type fakeOwners struct{ owners map[string]uuid.UUID }

func (f *fakeOwners) OwnersForTask(context.Context, uuid.UUID) (map[string]uuid.UUID, error) {
	return f.owners, nil
}

type ScorerSuite struct {
	suite.Suite
	dev, qa, pm, architect uuid.UUID
}

func TestScorerSuite(t *testing.T) { suite.Run(t, new(ScorerSuite)) }

func (s *ScorerSuite) SetupTest() {
	s.dev, s.qa, s.pm, s.architect = uuid.New(), uuid.New(), uuid.New(), uuid.New()
}

func (s *ScorerSuite) owners() *fakeOwners {
	return &fakeOwners{owners: map[string]uuid.UUID{
		"in_progress": s.dev,
		"in_qa":       s.qa,
		"pm_uat":      s.pm,
		"code_review": s.architect,
	}}
}

func (s *ScorerSuite) TestHumanUATFailurePenalisesDevQAAndPM() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	assignee := uuid.New() // deliberately nobody's span owner

	tracker.OnColumnTransition(context.Background(),
		domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &assignee},
		domain.TaskColumnHumanUAT, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 3)
	got := map[uuid.UUID]float64{}
	for _, a := range perf.applied {
		s.Equal(domain.ScoreEventHumanUATFailed, a.eventType)
		got[a.agentID] = a.delta
	}
	s.Equal(domain.ScoreDeltaHumanUATFailed, got[s.dev])
	s.Equal(domain.ScoreDeltaHumanUATFailed, got[s.qa])
	s.Equal(domain.ScoreDeltaHumanUATFailed, got[s.pm])
	s.NotContains(got, assignee)
}

func (s *ScorerSuite) TestPMUATFailurePenalisesDevAndQAOnly() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnPMUAT, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 2)
	for _, a := range perf.applied {
		s.Equal(domain.ScoreEventPMUATFailed, a.eventType)
		s.Equal(domain.ScoreDeltaPMUATFailed, a.delta)
		s.Contains([]uuid.UUID{s.dev, s.qa}, a.agentID)
	}
}

func (s *ScorerSuite) TestArchitectRejectionPenalisesDevOnly() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 1)
	s.Equal(s.dev, perf.applied[0].agentID)
	s.Equal(domain.ScoreEventRevisionRequested, perf.applied[0].eventType)
}

func (s *ScorerSuite) TestOneAgentHoldingTwoStagesIsChargedOnce() {
	perf := &fakePerfStore{}
	solo := uuid.New()
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(&fakeOwners{owners: map[string]uuid.UUID{
		"in_progress": solo,
		"in_qa":       solo,
		"pm_uat":      solo,
	}})

	tracker.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnHumanUAT, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 1)
	s.Equal(solo, perf.applied[0].agentID)
}

func (s *ScorerSuite) TestCompletionCreditsTheAssignee() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())
	assignee := uuid.New()

	tracker.OnColumnTransition(context.Background(),
		domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &assignee},
		domain.TaskColumnPMUAT, domain.TaskColumnDone)

	s.Require().Len(perf.applied, 1)
	s.Equal(assignee, perf.applied[0].agentID)
	s.Equal(domain.ScoreDeltaTaskCompleted, perf.applied[0].delta)
}

func (s *ScorerSuite) TestReviewEscapeChargesTheApprovingReviewer() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	tracker.SetSpans(s.owners())

	tracker.ApplyReviewEscape(context.Background(), domain.BoardTask{ID: uuid.New()}, s.architect)

	s.Require().Len(perf.applied, 1)
	s.Equal(s.architect, perf.applied[0].agentID)
	s.Equal(domain.ScoreEventReviewEscape, perf.applied[0].eventType)
	s.Equal(domain.ScoreDeltaReviewEscape, perf.applied[0].delta)
}

func (s *ScorerSuite) TestWithoutSpansFallsBackToAssignee() {
	perf := &fakePerfStore{}
	tracker := board.NewScoreTracker(perf)
	assignee := uuid.New()

	tracker.OnColumnTransition(context.Background(),
		domain.BoardTask{ID: uuid.New(), AssigneeAgentID: &assignee},
		domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision)

	s.Require().Len(perf.applied, 1)
	s.Equal(assignee, perf.applied[0].agentID)
}
