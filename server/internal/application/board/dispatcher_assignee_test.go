package board_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeAgentOwners struct {
	owners map[uuid.UUID]string
	err    error
	calls  int
}

func (f *fakeAgentOwners) AgentOwners(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		out[id] = f.owners[id]
	}
	return out, nil
}

type DispatcherAssigneeSuite struct {
	suite.Suite
	board  *fakeBoardConfigStore
	events *fakeEventStore
	runs   *fakeRunStore
	runner *fakeRunner
	disp   *board.Dispatcher
	owners *fakeAgentOwners

	ayseAgent   uuid.UUID
	mehmetAgent uuid.UUID
	sharedQA    uuid.UUID
}

func TestDispatcherAssigneeSuite(t *testing.T) {
	suite.Run(t, new(DispatcherAssigneeSuite))
}

func (s *DispatcherAssigneeSuite) SetupTest() {
	s.ayseAgent = uuid.New()
	s.mehmetAgent = uuid.New()
	s.sharedQA = uuid.New()

	s.board = &fakeBoardConfigStore{
		agentsByColumn: map[string][]uuid.UUID{
			"todo": {s.ayseAgent, s.mehmetAgent, s.sharedQA},
		},
	}
	s.events = &fakeEventStore{}
	s.runs = &fakeRunStore{}
	s.runner = &fakeRunner{}
	s.owners = &fakeAgentOwners{owners: map[uuid.UUID]string{
		s.ayseAgent:   "uid-ayse",
		s.mehmetAgent: "uid-mehmet",
	}}
	s.disp = board.NewDispatcher(s.board, s.events, s.runs, s.runner, true)
}

func (s *DispatcherAssigneeSuite) dispatch(assignee string) error {
	repositoryID := uuid.New()
	return s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:             uuid.New(),
			RepositoryID:   repositoryID,
			Title:          "t",
			Column:         domain.TaskColumnTodo,
			AssigneeUserID: assignee,
		},
		EventType: domain.BoardEventTaskCreated,
	})
}

func (s *DispatcherAssigneeSuite) dispatchedAgents() []uuid.UUID {
	var out []uuid.UUID
	for _, r := range s.runs.runs {
		out = append(out, r.AgentID)
	}
	return out
}

func (s *DispatcherAssigneeSuite) TestSoloTenantDispatchesEveryColumnAgent() {
	s.Require().NoError(s.dispatch(""))
	s.Len(s.dispatchedAgents(), 3, "a solo tenant dispatches exactly what the column subscribes")
	s.Zero(s.owners.calls, "with no roster wired, ownership is never even asked about")
}

func (s *DispatcherAssigneeSuite) TestUnassignedTaskOnATeamDispatchesEveryColumnAgent() {
	s.disp.SetAgentOwners(s.owners)
	s.Require().NoError(s.dispatch(""))
	s.Len(s.dispatchedAgents(), 3)
	s.Zero(s.owners.calls)
}

func (s *DispatcherAssigneeSuite) TestAssignedTaskDispatchesOnlyTheAssigneesAgentsAndSharedOnes() {
	s.disp.SetAgentOwners(s.owners)
	s.Require().NoError(s.dispatch("uid-ayse"))

	got := s.dispatchedAgents()
	s.Require().Len(got, 2)
	s.Contains(got, s.ayseAgent, "the assignee's own agent must run")
	s.Contains(got, s.sharedQA, "a shared agent stays eligible on an assigned card")
	s.NotContains(got, s.mehmetAgent, "another member's agent must never be woken")
}

func (s *DispatcherAssigneeSuite) TestTheOtherMemberGetsTheirOwnAgent() {
	s.disp.SetAgentOwners(s.owners)
	s.Require().NoError(s.dispatch("uid-mehmet"))

	got := s.dispatchedAgents()
	s.Require().Len(got, 2)
	s.Contains(got, s.mehmetAgent)
	s.Contains(got, s.sharedQA)
	s.NotContains(got, s.ayseAgent)
}

func (s *DispatcherAssigneeSuite) TestAssigneeWithNoOwnAgentsStillGetsSharedOnes() {
	s.disp.SetAgentOwners(s.owners)
	s.Require().NoError(s.dispatch("uid-nobody"))

	got := s.dispatchedAgents()
	s.Require().Len(got, 1)
	s.Equal(s.sharedQA, got[0])
}

func (s *DispatcherAssigneeSuite) TestOwnershipLookupFailureRefusesTheDispatch() {
	s.owners.err = errors.New("boom")
	s.disp.SetAgentOwners(s.owners)

	err := s.dispatch("uid-ayse")
	s.Require().Error(err)
	s.Empty(s.runs.runs, "no agent runs when ownership cannot be established")
}
