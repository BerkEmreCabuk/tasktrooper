package orchestrator_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type ExecutorMessagesSuite struct {
	suite.Suite
}

func (s *ExecutorMessagesSuite) TestBuildTaskMessages_IncludesLanguageAndToolGuidance() {
	agent := domain.Agent{
		ID:           uuid.New(),
		Name:         "general-coder",
		SystemPrompt: "You are a capable software engineer.",
	}
	tc := orchestrator.TaskContextForTest(domain.PlannerTask{
		ID: "t1", Title: "Dosya oluştur", Description: "helloworld.go yaz",
	})
	var mu sync.Mutex
	messages := orchestrator.BuildTaskMessagesForTest(
		nil, tc, nil, nil, agent, nil, &mu, nil, 0, "/data/ws/sub", "tr",
		domain.OrchestrationConfig{SubtaskHistoryMode: domain.SubtaskHistoryModeIsolated},
	)
	s.Require().Len(messages, 2)
	s.Equal(domain.RoleSystem, messages[0].Role)
	s.Contains(messages[0].Content, "You are a capable software engineer.")
	s.Contains(messages[0].Content, prompt.ToolSelectionGuidance())
	s.Contains(messages[0].Content, prompt.LanguageInstruction("tr"))
	s.Equal(domain.RoleUser, messages[1].Role)
	s.Contains(messages[1].Content, "Purpose:")
	s.Contains(messages[1].Content, "Goal:")
	s.Contains(messages[1].Content, "INTERNAL")
	s.Contains(messages[1].Content, "/data/ws/sub")
}

func (s *ExecutorMessagesSuite) TestBuildTaskMessages_IsolatedHistoryKeepsEveryUserTurn() {
	agent := domain.Agent{ID: uuid.New(), SystemPrompt: "engineer"}
	tc := orchestrator.TaskContextForTest(domain.PlannerTask{ID: "t1", Title: "T", Description: "D"})
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "first"},
		{Role: domain.RoleAssistant, Content: "done"},
		{Role: domain.RoleUser, Content: "second"},
	}
	var mu sync.Mutex
	messages := orchestrator.BuildTaskMessagesForTest(
		history, tc, nil, nil, agent, nil, &mu, nil, 0, "", "en",
		domain.OrchestrationConfig{SubtaskHistoryMode: domain.SubtaskHistoryModeIsolated},
	)
	s.Require().Len(messages, 5)
	s.Equal("first", messages[1].Content)
	s.Equal("done", messages[2].Content)
	// Dropping this turn made the agent read the session's opening instruction
	// as the current one, so "move that task" was executed as "create a task".
	s.Equal("second", messages[3].Content)
}

func (s *ExecutorMessagesSuite) TestBuildTaskMessages_IsolatedHistoryDropsToolChatterKeepsActionLedger() {
	taskID := uuid.New()
	ledger := domain.SessionActionDigest([]domain.SessionAction{{
		ToolName: "create_board_task", Verb: domain.ActionVerbCreated,
		EntityKind: domain.ActionEntityBoardTask, EntityID: &taskID, EntityKey: "TT-42",
	}})
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "session workspace: /data/ws"},
		{Role: domain.RoleSystem, Content: ledger},
		{Role: domain.RoleUser, Content: "move it to sprint"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1"}}},
		{Role: domain.RoleTool, Content: "{}", ToolCallID: "c1"},
	}

	out := orchestrator.IsolatedSubtaskHistoryForTest(history)

	s.Require().Len(out, 2)
	s.Equal(domain.RoleSystem, out[0].Role)
	s.Contains(out[0].Content, taskID.String())
	s.Equal("move it to sprint", out[1].Content)
}

func (s *ExecutorMessagesSuite) TestBuildTaskMessages_FullHistory() {
	agent := domain.Agent{ID: uuid.New(), SystemPrompt: "engineer"}
	tc := orchestrator.TaskContextForTest(domain.PlannerTask{ID: "t1", Title: "T", Description: "D"})
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "a"},
		{Role: domain.RoleUser, Content: "b"},
	}
	var mu sync.Mutex
	messages := orchestrator.BuildTaskMessagesForTest(
		history, tc, nil, nil, agent, nil, &mu, nil, 0, "", "en",
		domain.OrchestrationConfig{SubtaskHistoryMode: domain.SubtaskHistoryModeFull},
	)
	s.Len(messages, 4)
}

func TestExecutorMessagesSuite(t *testing.T) {
	suite.Run(t, new(ExecutorMessagesSuite))
}
