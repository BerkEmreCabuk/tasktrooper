package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSink captures the callbacks in order, so a test can assert both WHAT
// the stream reported and that it was reported as it arrived rather than
// collected and replayed at the end.
type recordingSink struct {
	sessionID string
	model     string
	turns     int
	texts     []string
	uses      []string
	results   []string
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

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// The happy path: everything the executor needs from a finished session has to
// come out of one pass over the stream — the session id it would resume, the
// turns for the transcript, the tool ledger the board's grounding gates read,
// and the usage the run row is stamped with.
func TestParseStreamReadsAFinishedSession(t *testing.T) {
	sink := &recordingSink{}

	out, err := parseStream(fixture(t, "success.jsonl"), sink)
	require.NoError(t, err)

	assert.Equal(t, "sess-abc123", out.SessionID, "the init event's session id is what a park would resume")
	assert.True(t, out.SawResult)
	assert.Equal(t, "success", out.Subtype)
	assert.False(t, out.IsError)
	assert.Equal(t, 6, out.NumTurns)
	assert.InDelta(t, 0.4213, out.CostUSD, 0.0001)
	assert.Equal(t, "Added the executor seam and wired it in. Build and vet are green.", out.Text,
		"the result event's own text is the run's answer, not the last assistant turn")

	assert.Equal(t, "sess-abc123", sink.sessionID)
	assert.Equal(t, "claude-opus-5", sink.model)
	assert.Equal(t, []string{
		"Reading the runner to see how the task is dispatched.",
		"Fixed the undefined symbol and the build is green.",
	}, sink.texts)
	assert.Equal(t, []string{"Read", "Bash"}, []string{
		strings.Split(sink.uses[0], "(")[0], strings.Split(sink.uses[1], "(")[0],
	})
	assert.Contains(t, sink.uses[0], `"file_path":"internal/application/board/runner.go"`,
		"the tool input is passed through as raw JSON, not re-encoded")
	assert.Equal(t, []string{"Read:ok", "Bash:error"}, sink.results,
		"a tool_result's is_error decides the ledger entry; the id maps it back to the tool's name")
	assert.Equal(t, 2, out.ToolCalls)
	assert.Equal(t, 1, out.ToolFailures)
	assert.Equal(t, 3, sink.turns, "one turn per assistant event, which is what brackets the trace")
}

// An assistant event that carries nothing this executor reports must not open a
// turn: the bracket exists to hold steps, and one with none is a node the reader
// has to expand to find out it is empty.
func TestParseStreamOpensNoTurnForAnEmptyAssistantEvent(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-3"}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"   "}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"here goes"}]}}`,
		`{"type":"result","subtype":"success","result":"done","session_id":"sess-3"}`,
	}, "\n")

	sink := &recordingSink{}
	_, err := parseStream(strings.NewReader(raw), sink)
	require.NoError(t, err)

	assert.Equal(t, 1, sink.turns)
	assert.Equal(t, []string{"here goes"}, sink.texts)
}

// Anthropic reports input_tokens with the cached share taken OUT; domain.Usage
// is the opposite (PromptTokens is the total, the cache figures are subsets of
// it). Getting the direction wrong under-reports every cached run, and a cached
// run is the normal case here.
func TestParseStreamNormalizesCacheTokensIntoTheTotal(t *testing.T) {
	out, err := parseStream(fixture(t, "success.jsonl"), &recordingSink{})
	require.NoError(t, err)

	assert.Equal(t, 1500+200+12000, out.Usage.PromptTokens, "prompt tokens must include both cache figures")
	assert.Equal(t, 800, out.Usage.CompletionTokens)
	assert.Equal(t, 12000, out.Usage.CacheReadTokens)
	assert.Equal(t, 200, out.Usage.CacheWriteTokens)
	assert.Equal(t, out.Usage.PromptTokens+out.Usage.CompletionTokens, out.Usage.TotalTokens)
}

// The stream is a subprocess's stdout: a wrapper script's warning or a
// half-flushed line can land in it. One unparseable line must cost that line,
// not the task.
func TestParseStreamSkipsLinesThatAreNotEvents(t *testing.T) {
	raw := strings.Join([]string{
		"npm warn: something irrelevant",
		`{"type":"system","subtype":"init","session_id":"sess-1"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","message":{"content":"a plain string, not blocks"}}`,
		`{"type":`,
		`{"type":"result","subtype":"success","result":"done","session_id":"sess-1"}`,
	}, "\n")

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(raw), sink)
	require.NoError(t, err)

	assert.True(t, out.SawResult)
	assert.Equal(t, "done", out.Text)
	assert.Equal(t, []string{"hi"}, sink.texts, "a string content block yields no text and no error")
}

// A stream with no terminal event is a killed process. Reporting its last
// assistant turn as the answer would hand the board a half-run to commit and
// hand off, which is the one outcome worse than a failed run.
func TestParseStreamReportsAMissingResultEvent(t *testing.T) {
	raw := `{"type":"system","subtype":"init","session_id":"sess-2"}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"working on it"}]}}`

	out, err := parseStream(strings.NewReader(raw), &recordingSink{})
	require.NoError(t, err)

	assert.False(t, out.SawResult)
	assert.Equal(t, "working on it", out.Text, "the partial text is still returned; the caller decides what it means")
}
