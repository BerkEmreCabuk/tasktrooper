package evolution

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// assertStrictObjectSchema mirrors the orchestrator package's check: OpenAI's
// strict json_schema mode rejects a schema unless every object sets
// "additionalProperties": false and lists every one of its own properties in
// "required" (there is no such thing as an optional field in strict mode).
// parseReflectionOutput itself never checks for missing keys — domain.
// ReflectionOutput is unmarshaled directly with no presence validation — so
// this is purely the API-level constraint, and the one a hand-edited
// reflectionOutputSchema would otherwise only break in production.
func assertStrictObjectSchema(t *testing.T, schema map[string]interface{}, path string) {
	t.Helper()
	typ, _ := schema["type"].(string)
	switch typ {
	case "object":
		if addl, ok := schema["additionalProperties"].(bool); !ok || addl != false {
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
			t.Errorf("%s: required has %d entries, properties has %d", path, len(required), len(props))
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

func TestReflectionOutputSchema_MarshalsAndSatisfiesStrictMode(t *testing.T) {
	schema := reflectionOutputSchema()

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("marshaled schema is empty")
	}
	var roundTrip map[string]interface{}
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal schema back: %v", err)
	}
	if roundTrip["type"] != "object" {
		t.Errorf("type = %v, want object", roundTrip["type"])
	}

	assertStrictObjectSchema(t, schema, "reflection")
}

// recordingLLM answers with one canned response and remembers every request it
// was handed, so a test can inspect what the caller actually asked for.
type recordingLLM struct {
	answer   string
	requests []domain.AgentRequest
}

func (r *recordingLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	r.requests = append(r.requests, req)
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: r.answer}}, nil
}

func (r *recordingLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (r *recordingLLM) Models(_ context.Context) ([]string, error) { return nil, nil }
func (r *recordingLLM) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, nil
}

// The reflection stage used to send domain.JSONResponseFormat() — bare
// {"type":"json_object"}, no schema — so a provider with strict structured
// output support had nothing to constrain decoding against. runLLM must now
// carry the real schema.
func TestRunLLM_RequestCarriesTheReflectionSchema(t *testing.T) {
	llm := &recordingLLM{answer: `{"self_assessment":"ok","skills":[],"rules":[],"memories":[],"reverts":[]}`}
	svc := &Service{
		llm: llm,
		cfg: domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 5},
	}

	output, raw, err := svc.runLLM(context.Background(), domain.Agent{Name: "backend-developer"}, "evidence text")
	if err != nil {
		t.Fatalf("runLLM: %v", err)
	}
	if output.SelfAssessment != "ok" {
		t.Errorf("self_assessment = %q, want %q", output.SelfAssessment, "ok")
	}
	if raw == "" {
		t.Error("raw output must be recorded even on success")
	}
	if len(llm.requests) != 1 {
		t.Fatalf("llm calls = %d, want 1 (no parse failure to retry)", len(llm.requests))
	}

	rf := llm.requests[0].ResponseFormat
	if rf == nil {
		t.Fatal("ResponseFormat is nil, want the reflection schema")
	}
	if rf.Type != domain.ResponseFormatJSONSchema {
		t.Errorf("ResponseFormat.Type = %q, want %q", rf.Type, domain.ResponseFormatJSONSchema)
	}
	if rf.Name == "" {
		t.Error("ResponseFormat.Name is empty; some providers reject an unnamed schema")
	}
	if rf.Schema == nil {
		t.Fatal("ResponseFormat.Schema is nil, want the reflection output schema")
	}
	if rf.Schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", rf.Schema["type"])
	}
}
