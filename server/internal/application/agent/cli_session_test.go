package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestCLISessionIsNilSafe(t *testing.T) {
	var s *CLISession
	require.Empty(t, s.ID(), "a holder that was never created reads as no session")
	require.NotPanics(t, func() { s.Set("sess-1") })
}

func TestCLISessionSetAndID(t *testing.T) {
	s := &CLISession{}
	require.Empty(t, s.ID())
	s.Set("sess-1")
	require.Equal(t, "sess-1", s.ID())
	s.Set("sess-2")
	require.Equal(t, "sess-2", s.ID(), "a later resumed turn's id replaces the earlier one")
}

func TestContextWithCLISessionRoundTrips(t *testing.T) {
	require.Nil(t, CLISessionFromContext(context.Background()), "a run that never plumbed a holder must not panic on lookup")

	s := &CLISession{}
	ctx := ContextWithCLISession(context.Background(), s)
	require.Same(t, s, CLISessionFromContext(ctx))
}

func TestResumeTailIsEmptyWithoutATrailingUserMessage(t *testing.T) {
	require.Empty(t, resumeTail(nil))
	require.Empty(t, resumeTail([]domain.Message{{Role: domain.RoleAssistant, Content: "done"}}))
	require.Empty(t, resumeTail([]domain.Message{
		{Role: domain.RoleUser, Content: "hi"},
		{Role: domain.RoleAssistant, Content: "done"},
		{Role: domain.RoleTool, Content: "result", Name: "x"},
	}), "the last message must be from the user")
}

func TestResumeTailIsEmptyWithNoAssistantTurn(t *testing.T) {
	require.Empty(t, resumeTail([]domain.Message{
		{Role: domain.RoleSystem, Content: "persona"},
		{Role: domain.RoleUser, Content: "do the work"},
	}), "a fresh run has nothing to resume: nothing has answered yet")
}

func TestResumeTailReturnsTheTrailingUserTextAfterTheLastAssistantTurn(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: "persona"},
		{Role: domain.RoleUser, Content: "implement the gate"},
		{Role: domain.RoleAssistant, Content: "done, the gate is wired"},
		{Role: domain.RoleUser, Content: "these criteria are still open:\n- the gate refuses a red build"},
	}
	require.Equal(t, "these criteria are still open:\n- the gate refuses a red build", resumeTail(messages))
}

func TestResumeTailJoinsMultipleTrailingUserMessagesAndSkipsOtherRoles(t *testing.T) {
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "implement the gate"},
		{Role: domain.RoleAssistant, Content: "done"},
		{Role: domain.RoleSystem, Content: "a findings digest, not a new instruction"},
		{Role: domain.RoleUser, Content: "first part"},
		{Role: domain.RoleTool, Content: "irrelevant", Name: "x"},
		{Role: domain.RoleUser, Content: "second part"},
	}
	require.Equal(t, "first part\n\nsecond part", resumeTail(messages))
}
