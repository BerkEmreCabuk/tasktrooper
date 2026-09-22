package orchestrator_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

// The id a "move it" turn addresses instead of opening a second board record.
var ledgerTaskID = uuid.MustParse("11111111-2222-3333-4444-555555555555")

func TestPipelineConversationHistory_SkipsSystemAndToolCalls(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "agent prompt"},
		{Role: domain.RoleUser, Content: "kişisel web sitesi için task oluştur"},
		{Role: domain.RoleAssistant, Content: "Soru 1: hedef kitle?"},
		{Role: domain.RoleAssistant, Content: "", ToolCalls: []domain.ToolCall{{ID: "1", Type: "function", Function: domain.FunctionCall{Name: "ask_user"}}}},
		{Role: domain.RoleUser, Content: "Genel halk"},
	}
	out := orchestrator.PipelineConversationHistoryForTest(history)
	requireLen := 3
	assert.Len(t, out, requireLen)
	assert.Equal(t, domain.RoleUser, out[0].Role)
	assert.Equal(t, "kişisel web sitesi için task oluştur", out[0].Content)
	assert.Equal(t, "Soru 1: hedef kitle?", out[1].Content)
	assert.Equal(t, "Genel halk", out[2].Content)
}

func TestPipelineConversationHistory_KeepsActionLedger(t *testing.T) {
	// The ledger is a system message, and every system message used to be dropped here.
	digest := domain.SessionActionDigest([]domain.SessionAction{{
		EntityKind: domain.ActionEntityBoardTask,
		Verb:       domain.ActionVerbCreated,
		EntityKey:  "DE-1",
		Title:      "Android uygulama bağlantısı ekleme",
		EntityID:   &ledgerTaskID,
		Column:     "backlog",
	}})
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "agent prompt"},
		{Role: domain.RoleUser, Content: "android linki ekle"},
		{Role: domain.RoleSystem, Content: digest},
		{Role: domain.RoleUser, Content: "analiz taskını board'a taşır mısın"},
	}

	out := orchestrator.PipelineConversationHistoryForTest(history)

	assert.Len(t, out, 3, "agent prompt dropped, ledger kept")
	assert.Equal(t, domain.RoleSystem, out[1].Role)
	assert.True(t, domain.IsSessionActionDigest(out[1].Content))
	assert.Contains(t, out[1].Content, "DE-1")
	assert.Contains(t, out[1].Content, ledgerTaskID.String(), "the id the move must address")
}

func TestBuildIntakeSystemPrompt_TreatsLedgerRecordsAsExistingWork(t *testing.T) {
	p := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{Lang: "tr"})

	assert.Contains(t, p, "records this conversation already created")
	assert.Contains(t, p, "It is not a new piece of work")
}

func TestAppendPipelineUserMessage_AvoidsDuplicateLatestUser(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "same"},
	}
	out := orchestrator.AppendPipelineUserMessageForTest(messages, "same")
	assert.Len(t, out, 1)
}

func TestAppendPipelineUserMessage_AppendsWhenDifferent(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleAssistant, Content: "clarify"},
	}
	out := orchestrator.AppendPipelineUserMessageForTest(messages, "answer")
	assert.Len(t, out, 2)
	assert.Equal(t, "answer", out[1].Content)
}

func TestBuildIntakeSystemPrompt_IncludesConversationRule(t *testing.T) {
	prompt := orchestrator.BuildIntakeSystemPromptForTest(orchestrator.IntakeOptions{Lang: "tr"})
	assert.Contains(t, prompt, "full conversation thread")
	assert.Contains(t, prompt, "Do not ask again")
}
