package context_test

import (
	"strings"
	"testing"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolCallTurn(id, name string) domain.Message {
	return domain.Message{
		Role:      domain.RoleAssistant,
		ToolCalls: []domain.ToolCall{{ID: id, Type: "function", Function: domain.FunctionCall{Name: name}}},
	}
}

func toolResultTurn(id, name, content string) domain.Message {
	return domain.Message{Role: domain.RoleTool, ToolCallID: id, Name: name, Content: content}
}

func clearableTranscript() []domain.Message {
	return []domain.Message{
		{Role: domain.RoleSystem, Content: "You are the backend developer."},
		{Role: domain.RoleUser, Content: "Find the auth bug."},
		toolCallTurn("c1", "grep_code"),
		toolResultTurn("c1", "grep_code", strings.Repeat("match line\n", 200)),
		toolCallTurn("c2", "read_file"),
		toolResultTurn("c2", "read_file", strings.Repeat("source line\n", 200)),
		toolCallTurn("c3", "run_terminal"),
		toolResultTurn("c3", "run_terminal", "FAIL TestAuth"),
	}
}

func TestClearToolResultsKeepsEveryCallPaired(t *testing.T) {
	in := clearableTranscript()

	out, cleared, _ := appcontext.ClearToolResults(in, 0)

	require.Len(t, out, len(in), "no message may be removed")
	assert.Equal(t, 3, cleared)
	for i := range in {
		assert.Equal(t, in[i].Role, out[i].Role, "message %d changed role", i)
		assert.Equal(t, in[i].ToolCallID, out[i].ToolCallID, "message %d lost its pairing", i)
		assert.Equal(t, in[i].ToolCalls, out[i].ToolCalls, "message %d lost its call record", i)
	}
}

func TestClearToolResultsDropsPayloadsAndNamesTheTool(t *testing.T) {
	out, _, freed := appcontext.ClearToolResults(clearableTranscript(), 0)

	assert.Equal(t, appcontext.ClearedToolResultNote("grep_code"), out[3].Content)
	assert.Contains(t, out[3].Content, "grep_code", "the note names the tool so the ledger stays readable")
	assert.Greater(t, freed, 2000, "two 200-line payloads must show up as a real saving")
}

// keepRecent counts tool results, not messages.
func TestClearToolResultsKeepsTheMostRecentResults(t *testing.T) {
	in := clearableTranscript()

	out, cleared, _ := appcontext.ClearToolResults(in, 2)

	assert.Equal(t, 1, cleared)
	assert.Equal(t, appcontext.ClearedToolResultNote("grep_code"), out[3].Content, "the oldest goes first")
	assert.Equal(t, in[5].Content, out[5].Content, "kept")
	assert.Equal(t, in[7].Content, out[7].Content, "kept")
}

func TestClearToolResultsLeavesNonToolMessagesAlone(t *testing.T) {
	in := clearableTranscript()

	out, _, _ := appcontext.ClearToolResults(in, 0)

	assert.Equal(t, in[0], out[0], "system")
	assert.Equal(t, in[1], out[1], "user")
	assert.Equal(t, in[2], out[2], "assistant tool call")
}

func TestClearToolResultsDropsImages(t *testing.T) {
	in := []domain.Message{
		toolCallTurn("c1", "browser_screenshot"),
		{
			Role: domain.RoleTool, ToolCallID: "c1", Name: "browser_screenshot",
			Content: "captured",
			Images:  []domain.ToolResultImage{{MediaType: "image/png", Data: strings.Repeat("A", 5000)}},
		},
	}

	out, cleared, freed := appcontext.ClearToolResults(in, 0)

	assert.Equal(t, 1, cleared)
	assert.Empty(t, out[1].Images)
	assert.Greater(t, freed, 4900, "the image bytes are the saving here")
}

// A second pass must report no saving, or the logs claim bytes never reclaimed.
func TestClearToolResultsIsIdempotent(t *testing.T) {
	once, firstCleared, firstFreed := appcontext.ClearToolResults(clearableTranscript(), 0)
	twice, secondCleared, secondFreed := appcontext.ClearToolResults(once, 0)

	assert.Equal(t, 3, firstCleared)
	assert.Greater(t, firstFreed, 0)
	assert.Equal(t, 0, secondCleared, "nothing left to clear")
	assert.Equal(t, 0, secondFreed)
	assert.Equal(t, once, twice)
}

func TestClearToolResultsDoesNotMutateTheInput(t *testing.T) {
	in := clearableTranscript()
	original := in[3].Content

	_, _, _ = appcontext.ClearToolResults(in, 0)

	assert.Equal(t, original, in[3].Content)
}
