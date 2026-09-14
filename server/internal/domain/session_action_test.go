package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type SessionActionSuite struct {
	suite.Suite
}

func (s *SessionActionSuite) TestClassifyKnownBoardTools() {
	spec, ok := domain.ClassifyBoardAction("create_board_task")
	s.Require().True(ok)
	s.Equal(domain.ActionVerbCreated, spec.Verb)
	s.Equal(domain.ActionEntityBoardTask, spec.Entity)

	spec, ok = domain.ClassifyBoardAction("move_board_task")
	s.Require().True(ok)
	s.Equal(domain.ActionVerbMoved, spec.Verb)
	s.Equal(domain.ActionEntityBoardTask, spec.Entity)
}

func (s *SessionActionSuite) TestClassifyIgnoresReadOnlyAndUnknownTools() {
	for _, name := range []string{"list_board_tasks", "get_board_summary", "read_file", "run_terminal", ""} {
		_, ok := domain.ClassifyBoardAction(name)
		s.False(ok, "tool %q must not be recorded as an action", name)
	}
}

func (s *SessionActionSuite) TestNewSessionActionExtractsBoardTaskIdentity() {
	taskID := uuid.New()
	result := `{"id":"` + taskID.String() + `","key":"TT-42","title":"Ops konsolu",` +
		`"column":"backlog","priority":"high","task_type":"task"}`

	action, ok := domain.NewSessionAction("create_board_task", result, false)
	s.Require().True(ok)
	s.Equal(domain.ActionEntityBoardTask, action.EntityKind)
	s.Require().NotNil(action.EntityID)
	s.Equal(taskID, *action.EntityID)
	s.Equal("TT-42", action.EntityKey)
	s.Equal("Ops konsolu", action.Title)
	s.Equal("backlog", action.Column)
	s.Equal("high", action.Priority)
	s.False(action.IsError)
}

func (s *SessionActionSuite) TestNewSessionActionKeepsFailuresOutOfTheLedger() {
	_, ok := domain.NewSessionAction("create_board_task", "invalid task_id", true)
	s.False(ok)
}

func (s *SessionActionSuite) TestNewSessionActionSkipsUnparseableResults() {
	_, ok := domain.NewSessionAction("create_board_task", "not json at all", false)
	s.False(ok)
}

func (s *SessionActionSuite) TestDigestNamesEveryEntityWithItsID() {
	taskID := uuid.New()
	actions := []domain.SessionAction{
		{
			ToolName: "create_board_task", Verb: domain.ActionVerbCreated,
			EntityKind: domain.ActionEntityBoardTask, EntityID: &taskID,
			EntityKey: "TT-42", Title: "Ops konsolu", Column: "backlog",
		},
		{
			ToolName: "move_board_task", Verb: domain.ActionVerbMoved,
			EntityKind: domain.ActionEntityBoardTask, EntityID: &taskID,
			EntityKey: "TT-42", Title: "Ops konsolu", Column: "sprint",
		},
	}

	digest := domain.SessionActionDigest(actions)
	s.Contains(digest, taskID.String())
	s.Contains(digest, "TT-42")
	s.Contains(digest, "Ops konsolu")
	s.Contains(digest, "backlog")
	s.Contains(digest, "sprint")
	// The whole point of the digest: stop the agent re-creating what it already made.
	s.Contains(digest, "do not create a new one")
}

func (s *SessionActionSuite) TestDigestIsEmptyWithoutActions() {
	s.Equal("", domain.SessionActionDigest(nil))
	s.Equal("", domain.SessionActionDigest([]domain.SessionAction{}))
}

func (s *SessionActionSuite) TestDigestKeepsOnlyTheMostRecentActions() {
	actions := make([]domain.SessionAction, 0, domain.SessionActionDigestLimit+5)
	for i := 0; i < cap(actions); i++ {
		id := uuid.New()
		actions = append(actions, domain.SessionAction{
			ToolName: "create_board_task", Verb: domain.ActionVerbCreated,
			EntityKind: domain.ActionEntityBoardTask, EntityID: &id,
			EntityKey: "TT-" + string(rune('a'+i)), Title: "t",
		})
	}
	digest := domain.SessionActionDigest(actions)
	s.NotContains(digest, actions[0].EntityID.String(), "oldest action must be dropped")
	s.Contains(digest, actions[len(actions)-1].EntityID.String(), "newest action must be kept")
}

func TestSessionActionSuite(t *testing.T) {
	suite.Run(t, new(SessionActionSuite))
}
