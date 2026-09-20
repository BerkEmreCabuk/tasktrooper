package antigravity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSink captures the callbacks in order, so a test can assert both WHAT
// the stream reported and that it was reported as it arrived.
type recordingSink struct {
	sessionID string
	model     string
	turns     int
	texts     []string
	uses      []string
	results   []string
	inits     []sessionInit
}

func (s *recordingSink) OnSession(sessionID, model string) { s.sessionID, s.model = sessionID, model }
func (s *recordingSink) OnTurn()                           { s.turns++ }
func (s *recordingSink) OnAssistantText(text string)       { s.texts = append(s.texts, text) }
func (s *recordingSink) OnToolUse(callID, name, arguments string) {
	s.uses = append(s.uses, name+"("+arguments+")")
}
func (s *recordingSink) OnToolResult(callID, name, content string, isError bool) {
	status := "ok"
	if isError {
		status = "error"
	}
	s.results = append(s.results, name+":"+status)
}
func (s *recordingSink) OnInit(init sessionInit) { s.inits = append(s.inits, init) }

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// The happy path: everything the executor needs from a finished session comes
// out of one pass over the stream.
func TestParseStreamReadsAFinishedSession(t *testing.T) {
	sink := &recordingSink{}

	out, err := parseStream(fixture(t, "success.jsonl"), sink)
	require.NoError(t, err)

	assert.Equal(t, "conv-abc123", out.SessionID)
	assert.True(t, out.SawResult)
	assert.Equal(t, "SUCCESS", out.Status)
	assert.False(t, out.IsError)
	assert.Equal(t, 6, out.NumTurns)
	assert.Equal(t, "Added the executor seam and wired it in. Build and vet are green.", out.Text,
		"the result event's own response is the run's answer, not the last text_delta")
	assert.Equal(t, 1500, out.Usage.PromptTokens)
	assert.Equal(t, 800, out.Usage.CompletionTokens)
	assert.Equal(t, 200, out.Usage.CacheReadTokens)
	assert.Equal(t, 2300, out.Usage.TotalTokens)

	assert.Equal(t, "conv-abc123", sink.sessionID)
	assert.Equal(t, []string{
		"Reading the runner to see how the task is dispatched.",
		"Fixed the undefined symbol and the build is green.",
	}, sink.texts)
	assert.Equal(t, []string{"read_file:ok", "run_terminal:error"}, sink.results)
	assert.Equal(t, 2, out.ToolCalls)
	assert.Equal(t, 1, out.ToolFailures)
	assert.Equal(t, 2, sink.turns, "one turn per distinct agent_response step index")

	require.Len(t, sink.inits, 1)
	assert.Equal(t, "conv-abc123", sink.inits[0].SessionID)
	assert.Equal(t, []string{"read_file", "run_terminal"}, sink.inits[0].Tools)
}

// Multiple text_delta chunks on the SAME step accumulate, matching the CLI's
// own "delta" naming — this is unlike opencode's full-snapshot text parts.
func TestParseStreamAccumulatesTextDeltasAcrossTheSameStep(t *testing.T) {
	raw := strings.Join([]string{
		`{"event":"init","conversation_id":"conv-1","init":{"cwd":"/workspace"}}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":0,"state":"ACTIVE","step_type":"agent_response","text_delta":"Hello, "}}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":0,"state":"ACTIVE","step_type":"agent_response","text_delta":"world."}}`,
	}, "\n")

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(raw), sink)
	require.NoError(t, err)

	assert.False(t, out.SawResult)
	assert.Equal(t, "Hello, world.", out.Text, "with no result event, the accumulated deltas are the fallback answer")
	assert.Equal(t, []string{"Hello, ", "world."}, sink.texts, "each delta is still reported to the sink as it arrives")
	assert.Equal(t, 1, sink.turns, "the same step index must not reopen a turn")
}

// A tool step reports ACTIVE (use) then either DONE (success) or ERROR
// (failure) — both drive the tool ledger, and only ERROR carries a message.
//
// A DONE/ERROR update with no tool_info AT ALL is silently dropped — see
// parseStream's `(upd.State == "DONE" || upd.State == "ERROR") &&
// upd.ToolInfo != nil` guard. That is a real quirk of the shipped parser, not
// a test artefact: the fixture below gives DONE an (empty-of-error)
// tool_info for exactly that reason, the same shape success.jsonl uses.
func TestParseStreamReportsToolActiveThenDoneOrError(t *testing.T) {
	raw := strings.Join([]string{
		`{"event":"init","conversation_id":"conv-1","init":{"cwd":"/workspace"}}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":0,"state":"ACTIVE","step_type":"tool","tool_name":"read_file","tool_info":{"parameters":{"path":"a.go"}}}}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":0,"state":"DONE","step_type":"tool","tool_name":"read_file","tool_info":{"parameters":{"path":"a.go"}}}}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":1,"state":"ACTIVE","step_type":"tool","tool_name":"run_terminal","tool_info":{"parameters":{"command":"go vet ./..."}}}}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":1,"state":"ERROR","step_type":"tool","tool_name":"run_terminal","tool_info":{"error":{"message":"vet failed"}}}}`,
	}, "\n")

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(raw), sink)
	require.NoError(t, err)

	assert.Equal(t, 2, out.ToolCalls)
	assert.Equal(t, 1, out.ToolFailures)
	assert.Equal(t, []string{"read_file", "run_terminal"}, []string{
		strings.Split(sink.uses[0], "(")[0], strings.Split(sink.uses[1], "(")[0],
	})
	assert.Equal(t, []string{"read_file:ok", "run_terminal:error"}, sink.results)
	assert.Equal(t, 0, sink.turns, "a tool step must never open a turn")
}

// A DONE/ERROR update that carries no tool_info at all never reaches the
// ledger — documented above, verified here so a future change to the guard
// shows up as a failing test rather than a silent behaviour change.
func TestParseStreamDropsAToolCompletionWithNoToolInfo(t *testing.T) {
	raw := strings.Join([]string{
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":0,"state":"ACTIVE","step_type":"tool","tool_name":"read_file"}}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":0,"state":"DONE","step_type":"tool","tool_name":"read_file"}}`,
	}, "\n")

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(raw), sink)
	require.NoError(t, err)

	assert.Equal(t, 0, out.ToolCalls, "the DONE with no tool_info is dropped, not counted")
	assert.Empty(t, sink.results)
}

// The stream is a subprocess's stdout: a wrapper script's warning or a
// half-flushed line can land in it. One unparseable line must cost that
// line, not the task.
func TestParseStreamSkipsLinesThatAreNotEvents(t *testing.T) {
	raw := strings.Join([]string{
		"warning: something irrelevant",
		`{"event":"init","conversation_id":"conv-1"}`,
		`{"event":"step_update","conversation_id":"conv-1","step_update":{"step_index":0,"state":"ACTIVE","step_type":"agent_response","text_delta":"hi"}}`,
		`{"event":`,
		`{"event":"result","conversation_id":"conv-1","result":{"status":"SUCCESS","response":"done"}}`,
	}, "\n")

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(raw), sink)
	require.NoError(t, err)

	assert.True(t, out.SawResult)
	assert.Equal(t, "done", out.Text)
	assert.Equal(t, []string{"hi"}, sink.texts)
}

// A result event whose status is not SUCCESS still finishes the stream, but
// IsError is set so the executor can fail the run with that status.
func TestParseStreamMarksANonSuccessResultAsAnError(t *testing.T) {
	raw := `{"event":"result","conversation_id":"conv-1","result":{"status":"BLOCKED","response":"needs approval"}}`

	out, err := parseStream(strings.NewReader(raw), &recordingSink{})
	require.NoError(t, err)

	assert.True(t, out.SawResult)
	assert.True(t, out.IsError)
	assert.Equal(t, "BLOCKED", out.Status)
}

// A stream with no terminal event at all is a killed process; the partial text
// is still returned so the executor's own "no result" failure message can
// quote it, but SawResult stays false so it is never mistaken for an answer.
func TestParseStreamReportsAMissingResultEvent(t *testing.T) {
	raw := `{"event":"init","conversation_id":"conv-2"}` + "\n" +
		`{"event":"step_update","conversation_id":"conv-2","step_update":{"step_index":0,"state":"ACTIVE","step_type":"agent_response","text_delta":"working on it"}}`

	out, err := parseStream(strings.NewReader(raw), &recordingSink{})
	require.NoError(t, err)

	assert.False(t, out.SawResult)
	assert.Equal(t, "working on it", out.Text)
}
