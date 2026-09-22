package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Stray commas mean nothing to the model and cost the whole run, so they are recovered instead of retried.
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

// A comma inside a string is content and must survive the repair.
func TestRepairJSONCommasLeavesStringsAlone(t *testing.T) {
	in := `{"description": "first, second, third", "ids": [,"a",]}`

	assert.Equal(t, `{"description": "first, second, third", "ids": ["a"]}`, repairJSONCommas(in))
}

// The repair must not rewrite documents that were never broken.
func TestRepairJSONCommasIsAnIdentityOnValidJSON(t *testing.T) {
	in := `{"a": [1, 2, {"b": "x, y"}], "c": {"d": true}}`

	assert.Equal(t, in, repairJSONCommas(in))
}

// Beyond the comma repair the strict parser's own error must surface.
func TestParseLLMJSONKeepsTheStrictErrorWhenRepairCannotHelp(t *testing.T) {
	var v struct {
		A string `json:"a"`
	}

	err := parseLLMJSON(`{"a": }`, &v)

	require.Error(t, err)
}

// The correction must quote the offending spot; the generic error made the model resend the same document.
func TestPipelineCorrectionShowsTheBrokenSpot(t *testing.T) {
	// A missing value is a failure the model itself has to fix.
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

// An empty completion must not echo an empty assistant turn: providers 400 a message with no content.
func TestPipelineCorrectionDoesNotEchoAnEmptyAnswer(t *testing.T) {
	msgs := pipelineCorrection("   ", assert.AnError)

	require.Len(t, msgs, 1)
	assert.Equal(t, "user", string(msgs[0].Role))
}

// An error without a position is about the schema, not a spot in the text.
func TestJSONErrorContextIsEmptyWithoutAnOffset(t *testing.T) {
	assert.Empty(t, jsonErrorContext(`{"a":1}`, assert.AnError))
}
