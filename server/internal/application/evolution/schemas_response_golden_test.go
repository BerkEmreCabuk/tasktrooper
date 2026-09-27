package evolution

import (
	"encoding/json"
	"testing"
)

// Golden fixtures pin the exact ResponseFormat JSON schemas reflect.go,
// golden.go and promote.go currently build in Go, before they move to
// catalog/system/schemas/*.json (see the prompt library program's WP9a).
// The move must keep every one of these byte-identical: json.Marshal on a
// map[string]interface{} always sorts object keys, so a schema loaded from
// the catalog file and re-marshaled matches this exact string as long as its
// content is unchanged — array order (required/enum lists) is NOT reordered
// by Marshal and must match too.
func TestGoldenResponseSchemas(t *testing.T) {
	cases := map[string]struct {
		schema map[string]interface{}
		want   string
	}{
		"agent_reflection": {
			schema: reflectionOutputSchema(),
			want:   `{"additionalProperties":false,"properties":{"memories":{"items":{"additionalProperties":false,"properties":{"action":{"enum":["create","delete"],"type":"string"},"category":{"type":"string"},"content":{"type":"string"},"memory_id":{"type":"string"},"reason":{"type":"string"}},"required":["action","memory_id","content","category","reason"],"type":"object"},"type":"array"},"reverts":{"items":{"additionalProperties":false,"properties":{"evolution_event_id":{"type":"string"},"reason":{"type":"string"}},"required":["evolution_event_id","reason"],"type":"object"},"type":"array"},"rules":{"items":{"additionalProperties":false,"properties":{"action":{"enum":["create","update","delete"],"type":"string"},"content":{"type":"string"},"name":{"type":"string"},"priority":{"type":"integer"},"reason":{"type":"string"},"rule_id":{"type":"string"}},"required":["action","rule_id","name","content","priority","reason"],"type":"object"},"type":"array"},"self_assessment":{"type":"string"},"skills":{"items":{"additionalProperties":false,"properties":{"action":{"enum":["create","update","delete"],"type":"string"},"category":{"type":"string"},"content":{"type":"string"},"description":{"type":"string"},"name":{"type":"string"},"reason":{"type":"string"},"skill_id":{"type":"string"},"source_urls":{"items":{"type":"string"},"type":"array"}},"required":["action","skill_id","name","description","category","content","source_urls","reason"],"type":"object"},"type":"array"}},"required":["self_assessment","skills","rules","memories","reverts"],"type":"object"}`,
		},
		"golden_gate_verdict": {
			schema: gateVerdictSchema(),
			want:   `{"additionalProperties":false,"properties":{"keep":{"type":"boolean"},"reason":{"type":"string"}},"required":["keep","reason"],"type":"object"}`,
		},
		"memory_promotion": {
			schema: promotionOutputSchema(),
			want:   `{"additionalProperties":false,"properties":{"promotions":{"items":{"additionalProperties":false,"properties":{"agents":{"items":{"type":"string"},"type":"array"},"category":{"type":"string"},"content":{"type":"string"},"description":{"type":"string"},"memory_id":{"type":"string"},"name":{"type":"string"}},"required":["memory_id","name","description","category","content","agents"],"type":"object"},"type":"array"}},"required":["promotions"],"type":"object"}`,
		},
		"memory_or_skill": {
			schema: saveClassificationSchema(),
			want:   `{"additionalProperties":false,"properties":{"category":{"type":"string"},"content":{"type":"string"},"description":{"type":"string"},"name":{"type":"string"},"skill":{"type":"boolean"}},"required":["skill","name","description","category","content"],"type":"object"}`,
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
