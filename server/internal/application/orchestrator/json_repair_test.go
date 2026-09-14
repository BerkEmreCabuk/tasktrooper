package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A model that writes `[,]` or a trailing comma killed the whole run: the strict
// parser rejects the document with "invalid character ',' looking for beginning
// of value", the stage retries with the error fed back, and the model repeats
// the same habit until the attempts are gone ("planner failed after 3 attempts").
// None of those commas mean anything, so the answer is recovered instead.
func TestParsePlannerOutputRecoversStrayCommas(t *testing.T) {
	content := `{
	  "ready": true,
	  "purpose": "p",
	  "goal": "g",
	  "questions": [],
	  "tasks": [
	    {
	      "id": "t1",
	      "title": "Add the Android button",
	      "description": "d",
	      "agent_id": "frontend-developer",
	      "skill_ids": [,],
	      "tool_names": ["edit_file",,"read_file"],
	      "depends_on": [],
	      "difficulty": "medium",
	      "parallel_group": 1,
	    },
	  ],
	}`

	out, err := parsePlannerOutput(content)

	require.NoError(t, err)
	require.Len(t, out.Tasks, 1)
	assert.Equal(t, "t1", out.Tasks[0].ID)
	assert.Equal(t, []string{"edit_file", "read_file"}, out.Tasks[0].ToolNames)
}

// The repair only removes commas that cannot legally be where they are. A comma
// inside a string is content — a task description eaten by the repair would be
// worse than the parse failure it fixes.
func TestRepairJSONCommasLeavesStringsAlone(t *testing.T) {
	in := `{"description": "first, second, third", "ids": [,"a",]}`

	assert.Equal(t, `{"description": "first, second, third", "ids": ["a"]}`, repairJSONCommas(in))
}

// Valid JSON must come back byte-for-byte, or the repair is rewriting documents
// that were never broken.
func TestRepairJSONCommasIsAnIdentityOnValidJSON(t *testing.T) {
	in := `{"a": [1, 2, {"b": "x, y"}], "c": {"d": true}}`

	assert.Equal(t, in, repairJSONCommas(in))
}

// A document broken in some other way still fails, with the strict parser's own
// error rather than a confusing one from the repaired text.
func TestParseLLMJSONKeepsTheStrictErrorWhenRepairCannotHelp(t *testing.T) {
	var v struct {
		A string `json:"a"`
	}

	err := parseLLMJSON(`{"a": }`, &v)

	require.Error(t, err)
}

// The retry only works if the model can see WHERE it went wrong. "invalid
// character ',' looking for beginning of value" names neither the place nor the
// mistake, and the model answered with the same document until the attempts ran
// out. The correction now quotes the offending spot with a marker.
func TestPipelineCorrectionShowsTheBrokenSpot(t *testing.T) {
	// A missing value: the comma repair cannot guess what belongs there, so this
	// is a failure the model itself has to fix.
	bad := `{"ready": true, "tasks": [{"id": }], "questions": []}`
	_, err := parsePlannerOutput(bad)
	require.Error(t, err)

	msgs := pipelineCorrection(bad, err)

	require.Len(t, msgs, 2)
	assert.Equal(t, bad, msgs[0].Content)
	assert.Contains(t, msgs[1].Content, "<<<HERE>>>")
	assert.Contains(t, msgs[1].Content, "character ")
	assert.Contains(t, msgs[1].Content, "two commas in a row")
}

// A correction for an EMPTY completion must not echo an empty assistant turn:
// providers reject a message with no content and no tool calls with a 400, so
// the retry would fail to send at all instead of asking for the answer again.
func TestPipelineCorrectionDoesNotEchoAnEmptyAnswer(t *testing.T) {
	msgs := pipelineCorrection("   ", assert.AnError)

	require.Len(t, msgs, 1)
	assert.Equal(t, "user", string(msgs[0].Role))
}

// An error with no position (a missing field, say) is about the schema, not a
// spot in the text — pointing at a character would be noise.
func TestJSONErrorContextIsEmptyWithoutAnOffset(t *testing.T) {
	assert.Empty(t, jsonErrorContext(`{"a":1}`, assert.AnError))
}
