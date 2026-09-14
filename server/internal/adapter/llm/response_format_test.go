package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestBuildResponseFormat_NilRequestFormatStaysNil(t *testing.T) {
	if got := buildResponseFormat(nil); got != nil {
		t.Errorf("buildResponseFormat(nil) = %+v, want nil", got)
	}
}

// The plain JSON mode (no schema) must still render as bare {"type":"json_object"}
// — every provider without schema support needs this untouched.
func TestBuildResponseFormat_PlainJSONObjectWhenNoSchema(t *testing.T) {
	got := buildResponseFormat(domain.JSONResponseFormat())
	if got == nil {
		t.Fatal("got nil, want a plain json_object response format")
	}
	if got.Type != domain.ResponseFormatJSONObject {
		t.Errorf("type = %q, want %q", got.Type, domain.ResponseFormatJSONObject)
	}
	if got.JSONSchema != nil {
		t.Errorf("json_schema = %+v, want absent on a schema-less request", got.JSONSchema)
	}
}

// A stage that sets .Schema must get the strict json_schema mode — this is
// the wiring that lets the planner (and intake, verifier, replanner) retire
// the parse-repair retry loop on providers that honor it.
func TestBuildResponseFormat_JSONSchemaWithStrictWhenSchemaSet(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"ready": map[string]interface{}{"type": "boolean"},
		},
		"required": []string{"ready"},
	}

	got := buildResponseFormat(domain.JSONSchemaResponseFormat("planner_output", schema))

	if got == nil {
		t.Fatal("got nil, want a json_schema response format")
	}
	if got.Type != domain.ResponseFormatJSONSchema {
		t.Errorf("type = %q, want %q", got.Type, domain.ResponseFormatJSONSchema)
	}
	if got.JSONSchema == nil {
		t.Fatal("json_schema is nil, want the schema payload")
	}
	if got.JSONSchema.Name != "planner_output" {
		t.Errorf("name = %q, want %q", got.JSONSchema.Name, "planner_output")
	}
	if !got.JSONSchema.Strict {
		t.Error("strict = false, want true — OpenAI-compatible structured output needs it to actually constrain decoding")
	}
	raw, err := json.Marshal(got.JSONSchema.Schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	wantRaw, _ := json.Marshal(schema)
	if string(raw) != string(wantRaw) {
		t.Errorf("schema = %s, want %s", raw, wantRaw)
	}
}

// OpenAI rejects an empty schema name, so a caller that forgets to name its
// schema must not send an empty string over the wire.
func TestBuildResponseFormat_EmptyNameDefaultsToResponse(t *testing.T) {
	got := buildResponseFormat(&domain.ResponseFormat{
		Type:   domain.ResponseFormatJSONSchema,
		Schema: map[string]interface{}{"type": "object"},
	})
	if got.JSONSchema.Name != "response" {
		t.Errorf("name = %q, want the %q fallback", got.JSONSchema.Name, "response")
	}
}

// End-to-end: the schema must actually reach the wire in the shape the
// OpenAI-compatible response_format parameter expects, not just the
// intermediate Go struct.
func TestOpenAICompatChatSendsJSONSchemaResponseFormatOnTheWire(t *testing.T) {
	var sent chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{}"}}]}`)
	}))
	defer srv.Close()

	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)
	schema := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]interface{}{"passed": map[string]interface{}{"type": "boolean"}},
		"required":             []string{"passed"},
	}
	if _, err := client.Chat(context.Background(), domain.AgentRequest{
		Messages:       []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
		ResponseFormat: domain.JSONSchemaResponseFormat("verification_result", schema),
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if sent.ResponseFormat == nil {
		t.Fatal("response_format was not sent")
	}
	if sent.ResponseFormat.Type != "json_schema" {
		t.Errorf("response_format.type = %q, want json_schema", sent.ResponseFormat.Type)
	}
	if sent.ResponseFormat.JSONSchema == nil {
		t.Fatal("response_format.json_schema was not sent")
	}
	if sent.ResponseFormat.JSONSchema.Name != "verification_result" {
		t.Errorf("response_format.json_schema.name = %q, want %q", sent.ResponseFormat.JSONSchema.Name, "verification_result")
	}
	if !sent.ResponseFormat.JSONSchema.Strict {
		t.Error("response_format.json_schema.strict = false on the wire, want true")
	}
	if len(sent.ResponseFormat.JSONSchema.Schema) == 0 {
		t.Error("response_format.json_schema.schema was not sent")
	}
}
