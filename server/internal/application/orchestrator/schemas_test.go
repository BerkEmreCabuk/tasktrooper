package orchestrator

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertStrictObjectSchema walks a JSON Schema built for
// domain.JSONSchemaResponseFormat and checks the two invariants OpenAI's
// strict json_schema mode enforces at the API boundary, not just at parse
// time: every object sets "additionalProperties": false, and every property
// it declares also appears in "required" (strict mode has no such thing as an
// optional property — see schemas.go's package doc). A schema that violates
// either is rejected by the provider before the model ever runs, so this is
// the regression a hand-edited schema.go would otherwise only surface in
// production.
func assertStrictObjectSchema(t *testing.T, schema map[string]interface{}, path string) {
	t.Helper()
	typ, _ := schema["type"].(string)
	switch typ {
	case "object":
		addl, ok := schema["additionalProperties"].(bool)
		if !ok || addl != false {
			t.Errorf("%s: additionalProperties = %v, want literal false", path, schema["additionalProperties"])
		}
		props, _ := schema["properties"].(map[string]interface{})
		required, _ := schema["required"].([]string)
		reqSet := make(map[string]bool, len(required))
		for _, r := range required {
			reqSet[r] = true
		}
		var missing []string
		for name := range props {
			if !reqSet[name] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: properties not listed in required: %v", path, missing)
		}
		if len(required) != len(props) {
			t.Errorf("%s: required has %d entries, properties has %d — every property must be required in strict mode", path, len(required), len(props))
		}
		for name, sub := range props {
			if subSchema, ok := sub.(map[string]interface{}); ok {
				assertStrictObjectSchema(t, subSchema, path+"."+name)
			}
		}
	case "array":
		if items, ok := schema["items"].(map[string]interface{}); ok {
			assertStrictObjectSchema(t, items, path+"[]")
		}
	}
}

func TestPipelineSchemas_MarshalAndSatisfyStrictMode(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"planner":   plannerOutputSchema(),
		"replanner": replannerOutputSchema(),
		"intake":    intakeOutputSchema(),
		"verifier":  verifierOutputSchema(),
	}
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(schema)
			require.NoError(t, err, "schema must marshal to valid JSON")
			assert.NotEmpty(t, raw)

			// Round-trips through the wire shape a provider actually receives —
			// map[string]interface{} nesting survives Marshal/Unmarshal cleanly.
			var roundTrip map[string]interface{}
			require.NoError(t, json.Unmarshal(raw, &roundTrip))
			assert.Equal(t, "object", roundTrip["type"])

			assertStrictObjectSchema(t, schema, name)
		})
	}
}

// The planner's own advertised shape includes "difficulty"; the replanner's
// prompt (buildReplannerSystemPrompt) never mentions it, and the two schemas
// must keep matching what each prompt actually promises the model.
func TestPlannerTaskSchema_HasDifficultyReplannerTaskSchemaDoesNot(t *testing.T) {
	plannerProps := plannerOutputSchema()["properties"].(map[string]interface{})
	plannerTask := plannerProps["tasks"].(map[string]interface{})["items"].(map[string]interface{})
	plannerTaskProps := plannerTask["properties"].(map[string]interface{})
	assert.Contains(t, plannerTaskProps, "difficulty")

	replannerProps := replannerOutputSchema()["properties"].(map[string]interface{})
	repairTask := replannerProps["tasks"].(map[string]interface{})["items"].(map[string]interface{})
	repairTaskProps := repairTask["properties"].(map[string]interface{})
	assert.NotContains(t, repairTaskProps, "difficulty")
}

// The planner and intake schemas both embed the ask_user question shape;
// mismatched copies would silently diverge from prompt.AskUserQuestionJSONShape.
func TestClarificationQuestionSchema_SharedByPlannerAndIntake(t *testing.T) {
	plannerProps := plannerOutputSchema()["properties"].(map[string]interface{})
	plannerQ := plannerProps["questions"].(map[string]interface{})["items"].(map[string]interface{})

	intakeProps := intakeOutputSchema()["properties"].(map[string]interface{})
	intakeQ := intakeProps["questions"].(map[string]interface{})["items"].(map[string]interface{})

	plannerQJSON, err := json.Marshal(plannerQ)
	require.NoError(t, err)
	intakeQJSON, err := json.Marshal(intakeQ)
	require.NoError(t, err)
	assert.JSONEq(t, string(plannerQJSON), string(intakeQJSON))
}
