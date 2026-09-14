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

// streamServer replays a canned SSE body and hands the request it was sent back
// to the test, which is the only way to see what the client actually asked for.
func streamServer(t *testing.T, frames []string) (*httptest.Server, *chatRequest) {
	t.Helper()
	var sent chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &sent
}

var streamTestTools = []domain.ToolDefinition{{
	Type: "function",
	Function: domain.FunctionDefinition{
		Name:        "read_file",
		Description: "Read a file",
		Parameters:  map[string]interface{}{"type": "object"},
	},
}}

// The whole point of streaming with tools: a streamed turn must be able to plan
// tool calls. It could not while the stream request went out tool-less, so the
// loop paid for a second, buffered request just to find out what the model
// wanted to do.
func TestOpenAICompatChatStreamSendsToolsAndUsageOption(t *testing.T) {
	srv, sent := streamServer(t, []string{`{"choices":[{"delta":{"content":"hi"}}]}`})
	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)

	if _, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
		Tools:    streamTestTools,
	}, func(string) {}); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if len(sent.Tools) != 1 || sent.Tools[0].Function.Name != "read_file" {
		t.Errorf("streamed request carried tools %+v, want the one tool it was given", sent.Tools)
	}
	if sent.ToolChoice != "auto" {
		t.Errorf("tool_choice = %q, want %q — some runtimes emit no tool calls without it", sent.ToolChoice, "auto")
	}
	if !sent.Stream {
		t.Error("stream = false on a streamed request")
	}
	// Without this the provider sends no usage at all and every streamed turn
	// is billed as zero tokens.
	if sent.StreamOptions == nil || !sent.StreamOptions.IncludeUsage {
		t.Errorf("stream_options = %+v, want include_usage true", sent.StreamOptions)
	}
}

// A non-streamed request must not grow the option: it is stream-only, and
// strict servers reject parameters that do not belong.
func TestOpenAICompatChatOmitsStreamOptions(t *testing.T) {
	var sent chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sent)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)
	if _, err := client.Chat(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if sent.StreamOptions != nil {
		t.Errorf("stream_options = %+v on a non-streamed request, want absent", sent.StreamOptions)
	}
}

// Tool calls arrive in fragments: the id and name once, the arguments spread
// over as many chunks as the provider feels like. Read one fragment at a time
// they are unparseable JSON; only the reassembled call is a call.
func TestOpenAICompatChatStreamAssemblesToolCallFragments(t *testing.T) {
	srv, _ := streamServer(t, []string{
		`{"choices":[{"delta":{"role":"assistant","content":"Let me look. "}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":": \"main.go\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"read_file","arguments":"{\"path\": \"go.mod\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":34,"total_tokens":154}}`,
	})
	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)

	var streamed strings.Builder
	resp, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "read main.go"}},
		Tools:    streamTestTools,
	}, func(tok string) { streamed.WriteString(tok) })
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	// Only text is the user's business. A forwarded argument fragment would
	// print half-written JSON into the answer.
	if got := streamed.String(); got != "Let me look. " {
		t.Errorf("streamed %q, want only the text delta", got)
	}
	if got := resp.Message.Content; got != "Let me look. " {
		t.Errorf("content = %q, want the text the model actually said", got)
	}

	if len(resp.Message.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2: %+v", len(resp.Message.ToolCalls), resp.Message.ToolCalls)
	}
	first := resp.Message.ToolCalls[0]
	if first.ID != "call_a" || first.Function.Name != "read_file" {
		t.Errorf("first call = %+v, want call_a/read_file", first)
	}
	if got, want := first.Function.Arguments, `{"path": "main.go"}`; got != want {
		t.Errorf("first call arguments = %q, want %q — the fragments were not joined", got, want)
	}
	if got, want := resp.Message.ToolCalls[1].Function.Arguments, `{"path": "go.mod"}`; got != want {
		t.Errorf("second call arguments = %q, want %q", got, want)
	}

	// The usage chunk is the one with an empty choices array; skipping it as
	// choiceless is how a streamed turn came back billed at zero.
	if resp.Usage.PromptTokens != 120 || resp.Usage.CompletionTokens != 34 || resp.Usage.TotalTokens != 154 {
		t.Errorf("usage = %+v, want the final chunk's counts", resp.Usage)
	}
}

// Providers that ignore stream_options send no usage block. That is a zero
// usage, not a failure.
func TestOpenAICompatChatStreamSurvivesMissingUsage(t *testing.T) {
	srv, _ := streamServer(t, []string{`{"choices":[{"delta":{"content":"done"}}]}`})
	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)

	resp, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	}, func(string) {})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Usage != (domain.Usage{}) {
		t.Errorf("usage = %+v, want zero when the provider sent none", resp.Usage)
	}
	if resp.Message.Content != "done" {
		t.Errorf("content = %q, want %q", resp.Message.Content, "done")
	}
	if len(resp.Message.ToolCalls) != 0 {
		t.Errorf("tool calls = %+v, want none", resp.Message.ToolCalls)
	}
}

// Some OpenAI-compatible servers omit index entirely. Keying everything at zero
// would splice two distinct calls' arguments into one unparseable string, so a
// fresh id under the same key has to start a new call.
func TestOpenAICompatChatStreamSeparatesUnindexedToolCalls(t *testing.T) {
	srv, _ := streamServer(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"id":"call_a","function":{"name":"read_file","arguments":"{\"path\": \"a.go\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"id":"call_b","function":{"name":"read_file","arguments":"{\"path\": \"b.go\"}"}}]}}]}`,
	})
	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)

	resp, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "read both"}},
		Tools:    streamTestTools,
	}, func(string) {})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if len(resp.Message.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2 kept apart: %+v", len(resp.Message.ToolCalls), resp.Message.ToolCalls)
	}
	if got, want := resp.Message.ToolCalls[0].Function.Arguments, `{"path": "a.go"}`; got != want {
		t.Errorf("first call arguments = %q, want %q", got, want)
	}
	if got, want := resp.Message.ToolCalls[1].Function.Arguments, `{"path": "b.go"}`; got != want {
		t.Errorf("second call arguments = %q, want %q", got, want)
	}
}
