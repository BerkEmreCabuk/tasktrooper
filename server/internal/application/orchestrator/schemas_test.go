package orchestrator

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Checks what strict json_schema mode enforces at the provider boundary: every object
// sets additionalProperties=false and every declared property is required.
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

			var roundTrip map[string]interface{}
			require.NoError(t, json.Unmarshal(raw, &roundTrip))
			assert.Equal(t, "object", roundTrip["type"])

			assertStrictObjectSchema(t, schema, name)
		})
	}
}

// The planner's shape promises difficulty; the replanner's prompt never mentions it.
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

// Both schemas embed the ask_user question shape; keep them on prompt.AskUserQuestionJSONShape.
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
