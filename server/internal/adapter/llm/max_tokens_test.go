package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// --- LM Studio (OpenAI-compatible) ---

// openAICompatCaptureBody runs one buffered Chat call and returns the raw request
// bytes, so a field's ABSENCE — not just its zero value, which a field that
// was simply never sent would also unmarshal to — can be asserted.
func openAICompatCaptureBody(t *testing.T, req domain.AgentRequest) string {
	t.Helper()
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)
	if _, err := client.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	return string(raw)
}

// A caller that sets MaxTokens (the agent loop enforcing its context budget's
// output reserve, or a utility call capping its own answer) must see it reach
// the wire — OpenAI-compatible endpoints otherwise apply no output cap at all.
func TestOpenAICompatChatSendsMaxTokensWhenSet(t *testing.T) {
	body := openAICompatCaptureBody(t, domain.AgentRequest{
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		MaxTokens: 4096,
	})
	if !strings.Contains(body, `"max_tokens":4096`) {
		t.Errorf("request body = %s, want it to carry max_tokens:4096", body)
	}
}

// A pipeline stage (planner, intake, verifier, ...) that never sets MaxTokens
// must not send the field at all — omitempty is what lets the endpoint's own
// default apply, exactly as every request behaved before this field existed.
func TestOpenAICompatChatOmitsMaxTokensWhenUnset(t *testing.T) {
	body := openAICompatCaptureBody(t, domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if strings.Contains(body, "max_tokens") {
		t.Errorf("request body = %s, want no max_tokens field when the caller set none", body)
	}
}

// The streamed path builds its payload separately from the buffered one; the
// field has to reach the wire there too.
func TestOpenAICompatChatStreamSendsMaxTokensWhenSet(t *testing.T) {
	srv, sent := streamServer(t, []string{`{"choices":[{"delta":{"content":"hi"}}]}`})
	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)

	if _, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		MaxTokens: 2048,
	}, func(string) {}); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if sent.MaxTokens != 2048 {
		t.Errorf("max_tokens = %d, want 2048", sent.MaxTokens)
	}
}

// --- Anthropic ---

// resolveAnthropicMaxTokens is the shared decision both Chat and ChatStream
// apply after buildAnthropicRequest; a plain table test proves it without a
// round trip.
func TestResolveAnthropicMaxTokens(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		override int
		want     int
	}{
		{"override wins over the modern default", "claude-sonnet-4", 1234, 1234},
		{"no override falls back to the modern default", "claude-sonnet-4", 0, anthropicDefaultMaxTokens},
		{"no override falls back to the legacy cap on claude-3-opus", "claude-3-opus-20240229", 0, anthropicLegacyMaxTokens},
		{"no override falls back to the legacy cap on claude-3-haiku", "claude-3-haiku-20240307", 0, anthropicLegacyMaxTokens},
		{"override still wins on a legacy model", "claude-3-opus-20240229", 500, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveAnthropicMaxTokens(tt.model, tt.override); got != tt.want {
				t.Errorf("resolveAnthropicMaxTokens(%q, %d) = %d, want %d", tt.model, tt.override, got, tt.want)
			}
		})
	}
}

// anthropicCaptureRequest runs one Chat call against a canned server and hands
// back the request the adapter actually sent on the wire.
func anthropicCaptureRequest(t *testing.T, req domain.AgentRequest) anthropicRequest {
	t.Helper()
	var sent anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"usage":{}}`)
	}))
	defer srv.Close()

	client := NewAnthropicClient(srv.URL, "claude-sonnet-4", "k", 5*time.Second)
	if _, err := client.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	return sent
}

// End to end: an override set on the domain request must arrive on the wire
// as max_tokens, not just inside the unexported struct the builder returns.
func TestAnthropicChatRequestBuiltWithMaxTokensOverride(t *testing.T) {
	sent := anthropicCaptureRequest(t, domain.AgentRequest{
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		MaxTokens: 1234,
	})
	if sent.MaxTokens != 1234 {
		t.Errorf("max_tokens = %d, want 1234 (the override)", sent.MaxTokens)
	}
}

// Without an override the request must still carry the adapter's own
// per-model default — Anthropic has no "no cap" mode, so this field can never
// simply be omitted the way the OpenAI-compatible one can.
func TestAnthropicChatRequestBuiltWithoutMaxTokensOverride(t *testing.T) {
	sent := anthropicCaptureRequest(t, domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if sent.MaxTokens != anthropicDefaultMaxTokens {
		t.Errorf("max_tokens = %d, want the adapter default %d", sent.MaxTokens, anthropicDefaultMaxTokens)
	}
}

// The streamed path builds and overrides MaxTokens separately from the
// buffered one (ChatStream has its own buildAnthropicRequest call); it needs
// its own proof the override reaches the wire.
func TestAnthropicChatStreamRequestBuiltWithMaxTokensOverride(t *testing.T) {
	var sent anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{}}}\n\n"+
			"data: {\"type\":\"message_delta\",\"usage\":{}}\n\n")
	}))
	defer srv.Close()

	client := NewAnthropicClient(srv.URL, "claude-sonnet-4", "k", 5*time.Second)
	if _, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages:  []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		MaxTokens: 777,
	}, func(string) {}); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if sent.MaxTokens != 777 {
		t.Errorf("max_tokens = %d, want 777 (the override)", sent.MaxTokens)
	}
}

// --- Gemini ---

// buildGeminiConfig is a pure function, so the cap can be asserted without a
// genai client or any network access.
func TestBuildGeminiConfigSetsMaxOutputTokensWhenPositive(t *testing.T) {
	config := buildGeminiConfig(nil, nil, nil, 512)
	if config.MaxOutputTokens != 512 {
		t.Errorf("max output tokens = %d, want 512", config.MaxOutputTokens)
	}
}

// Zero must leave the SDK field at its own zero value rather than sending an
// explicit 0, which the API would read as "generate nothing".
func TestBuildGeminiConfigLeavesMaxOutputTokensZeroWhenUnset(t *testing.T) {
	config := buildGeminiConfig(nil, nil, nil, 0)
	if config.MaxOutputTokens != 0 {
		t.Errorf("max output tokens = %d, want 0 (provider default)", config.MaxOutputTokens)
	}
}
