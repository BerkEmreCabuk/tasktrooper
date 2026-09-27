package orchestrator

import (
	"encoding/json"
	"testing"
)

// Golden fixtures pin the exact ResponseFormat JSON schemas.go currently
// builds in Go, before they move to catalog/system/schemas/*.json (see the
// prompt library program's WP9a). The move must keep every one of these
// byte-identical: json.Marshal on a map[string]interface{} always sorts
// object keys, so a schema loaded from the catalog file and re-marshaled
// matches this exact string as long as its content is unchanged — array
// order (required/enum lists) is NOT reordered by Marshal and must match too.
func TestGoldenResponseSchemas(t *testing.T) {
	cases := map[string]struct {
		schema map[string]interface{}
		want   string
	}{
		"goal_intake": {
			schema: intakeOutputSchema(),
			want:   `{"additionalProperties":false,"properties":{"constraints":{"items":{"type":"string"},"type":"array"},"goal":{"type":"string"},"purpose":{"type":"string"},"questions":{"items":{"additionalProperties":false,"properties":{"allow_multiple":{"type":"boolean"},"id":{"type":"string"},"options":{"items":{"additionalProperties":false,"properties":{"id":{"type":"string"},"label":{"type":"string"}},"required":["id","label"],"type":"object"},"type":"array"},"prompt":{"type":"string"}},"required":["id","prompt","allow_multiple","options"],"type":"object"},"type":"array"},"ready":{"type":"boolean"}},"required":["ready","purpose","goal","constraints","questions"],"type":"object"}`,
		},
		"planner_output": {
			schema: plannerOutputSchema(),
			want:   `{"additionalProperties":false,"properties":{"questions":{"items":{"additionalProperties":false,"properties":{"allow_multiple":{"type":"boolean"},"id":{"type":"string"},"options":{"items":{"additionalProperties":false,"properties":{"id":{"type":"string"},"label":{"type":"string"}},"required":["id","label"],"type":"object"},"type":"array"},"prompt":{"type":"string"}},"required":["id","prompt","allow_multiple","options"],"type":"object"},"type":"array"},"ready":{"type":"boolean"},"summary":{"type":"string"},"tasks":{"items":{"additionalProperties":false,"properties":{"agent_id":{"type":"string"},"depends_on":{"items":{"type":"string"},"type":"array"},"description":{"type":"string"},"difficulty":{"enum":["easy","hard"],"type":"string"},"id":{"type":"string"},"parallel_group":{"type":"integer"},"skill_ids":{"items":{"type":"string"},"type":"array"},"subtask_rules":{"items":{"type":"string"},"type":"array"},"title":{"type":"string"},"tool_names":{"items":{"type":"string"},"type":"array"}},"required":["id","title","description","agent_id","skill_ids","tool_names","subtask_rules","depends_on","difficulty","parallel_group"],"type":"object"},"type":"array"}},"required":["ready","summary","questions","tasks"],"type":"object"}`,
		},
		"replan_output": {
			schema: replannerOutputSchema(),
			want:   `{"additionalProperties":false,"properties":{"summary":{"type":"string"},"tasks":{"items":{"additionalProperties":false,"properties":{"agent_id":{"type":"string"},"depends_on":{"items":{"type":"string"},"type":"array"},"description":{"type":"string"},"id":{"type":"string"},"parallel_group":{"type":"integer"},"skill_ids":{"items":{"type":"string"},"type":"array"},"subtask_rules":{"items":{"type":"string"},"type":"array"},"title":{"type":"string"},"tool_names":{"items":{"type":"string"},"type":"array"}},"required":["id","title","description","agent_id","skill_ids","tool_names","subtask_rules","depends_on","parallel_group"],"type":"object"},"type":"array"}},"required":["summary","tasks"],"type":"object"}`,
		},
		"verification_result": {
			schema: verifierOutputSchema(),
			want:   `{"additionalProperties":false,"properties":{"issues":{"items":{"type":"string"},"type":"array"},"passed":{"type":"boolean"},"summary":{"type":"string"}},"required":["passed","issues","summary"],"type":"object"}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(tc.schema)
			if err != nil {
				t.Fatalf("marshal %s schema: %v", name, err)
			}
			if string(raw) != tc.want {
				t.Fatalf("%s schema changed\ngot:  %s\nwant: %s", name, raw, tc.want)
			}
		})
	}
}
