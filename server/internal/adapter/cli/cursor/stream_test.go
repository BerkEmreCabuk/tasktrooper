package cursor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSink captures every callback parseStream makes, so a test can
// assert both the returned outcome and that the session was actually
// narrated as it happened, not just summarised at the end.
type recordingSink struct {
	sessions  []string
	turns     int
	texts     []string
	toolUses  []string
	toolDones []string
}

func (s *recordingSink) OnSession(sessionID, model string) { s.sessions = append(s.sessions, sessionID) }
func (s *recordingSink) OnTurn()                            { s.turns++ }
func (s *recordingSink) OnAssistantText(text string)        { s.texts = append(s.texts, text) }
func (s *recordingSink) OnToolUse(callID, name, arguments string) {
	s.toolUses = append(s.toolUses, name)
}
func (s *recordingSink) OnToolResult(callID, name, content string, isError bool) {
	label := name
	if isError {
		label += ":error"
	}
	s.toolDones = append(s.toolDones, label)
}

func TestParseStreamReadsAFinishedSession(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-1","model":"sonnet-4-thinking"}`,
		`{"type":"assistant","session_id":"sess-1","message":{"content":[{"type":"text","text":"Looking at the code."}]}}`,
		`{"type":"tool_call","session_id":"sess-1","call_id":"call_1","subtype":"started","tool_call":{"readToolCall":{"args":{"path":"a.go"}}}}`,
		`{"type":"tool_call","session_id":"sess-1","call_id":"call_1","subtype":"completed","tool_call":{"readToolCall":{"result":{"success":{"content":"package a"}}}}}`,
		`{"type":"result","subtype":"success","session_id":"sess-1","is_error":false,"result":"Done."}`,
	}, "\n")

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)

	assert.Equal(t, "sess-1", out.SessionID)
	assert.Equal(t, "sonnet-4-thinking", out.Model)
	assert.Equal(t, "Done.", out.Text, "the result event's own text overrides the last assistant line")
	assert.False(t, out.IsError)
	assert.Equal(t, "success", out.Status)
	assert.True(t, out.SawResult)
	assert.Equal(t, 1, out.ToolCalls)
	assert.Equal(t, 0, out.ToolFailures)

	assert.Equal(t, []string{"sess-1"}, sink.sessions)
	assert.Equal(t, 1, sink.turns)
	assert.Equal(t, []string{"Looking at the code."}, sink.texts)
	assert.Equal(t, []string{"read"}, sink.toolUses, "the ToolCall suffix must be stripped from the dynamic key")
	assert.Equal(t, []string{"read"}, sink.toolDones)
}

func TestParseStreamCountsAFailedToolCall(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-2"}`,
		`{"type":"tool_call","session_id":"sess-2","call_id":"call_1","subtype":"started","tool_call":{"shellToolCall":{"args":{"command":"go build ./..."}}}}`,
		`{"type":"tool_call","session_id":"sess-2","call_id":"call_1","subtype":"completed","tool_call":{"shellToolCall":{"result":{"error":{"message":"exit status 2"}}}}}`,
		`{"type":"result","subtype":"success","session_id":"sess-2","is_error":false,"result":"ok"}`,
	}, "\n")

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)

	assert.Equal(t, 1, out.ToolCalls)
	assert.Equal(t, 1, out.ToolFailures)
	assert.Equal(t, []string{"shell:error"}, sink.toolDones)
}

func TestParseStreamSkipsLinesThatAreNotEvents(t *testing.T) {
	body := strings.Join([]string{
		"",
		"not json at all",
		`{"type":"system","subtype":"init","session_id":"sess-3"}`,
		`{"type":"result","subtype":"success","session_id":"sess-3","is_error":false,"result":"ok"}`,
	}, "\n")

	out, err := parseStream(strings.NewReader(body), &recordingSink{})
	require.NoError(t, err)
	assert.Equal(t, "sess-3", out.SessionID)
	assert.True(t, out.SawResult)
}

func TestParseStreamReportsAMissingResultEvent(t *testing.T) {
	body := `{"type":"system","subtype":"init","session_id":"sess-4"}` + "\n" +
		`{"type":"assistant","session_id":"sess-4","message":{"content":[{"type":"text","text":"still working"}]}}`

	out, err := parseStream(strings.NewReader(body), &recordingSink{})
	require.NoError(t, err)
	assert.False(t, out.SawResult, "a session cut off mid-stream must not look finished")
}

func TestParseStreamMarksAnErrorResult(t *testing.T) {
	body := `{"type":"system","subtype":"init","session_id":"sess-5"}` + "\n" +
		`{"type":"result","subtype":"error_during_execution","session_id":"sess-5","is_error":true,"result":"blocked"}`

	out, err := parseStream(strings.NewReader(body), &recordingSink{})
	require.NoError(t, err)
	assert.True(t, out.SawResult)
	assert.True(t, out.IsError)
	assert.Equal(t, "error_during_execution", out.Status)
	assert.Equal(t, "blocked", out.Text)
}
