package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// anthropicServer replays a canned response body for one /messages call.
func anthropicServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The buffered path end to end: what the API sends must arrive at the caller
// already normalized, not just inside the unexported struct.
func TestAnthropicChatReportsCacheUsage(t *testing.T) {
	srv := anthropicServer(t, "application/json", `{
		"content": [{"type": "text", "text": "ok"}],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 25,
			"cache_read_input_tokens": 900,
			"cache_creation_input_tokens": 50
		}
	}`)
	client := NewAnthropicClient(srv.URL, "claude-sonnet-4", "k", 5*time.Second)

	resp, err := client.Chat(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	want := domain.Usage{
		PromptTokens:     1050,
		CompletionTokens: 25,
		TotalTokens:      1075,
		CacheReadTokens:  900,
		CacheWriteTokens: 50,
	}
	if resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}
}

// The streamed path splits usage across two events: message_start carries the
// prompt side (cache included), message_delta the output. Reading only the
// delta billed every cached turn as if nothing had been cached.
func TestAnthropicChatStreamReportsCacheUsage(t *testing.T) {
	srv := anthropicServer(t, "text/event-stream", `data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":900,"cache_creation_input_tokens":50}}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

data: {"type":"message_delta","usage":{"output_tokens":25}}

`)
	client := NewAnthropicClient(srv.URL, "claude-sonnet-4", "k", 5*time.Second)

	resp, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	}, func(string) {})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}

	want := domain.Usage{
		PromptTokens:     1050,
		CompletionTokens: 25,
		TotalTokens:      1075,
		CacheReadTokens:  900,
		CacheWriteTokens: 50,
	}
	if resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}
	if resp.Message.Content != "ok" {
		t.Errorf("content = %q, want %q", resp.Message.Content, "ok")
	}
}

// Newer API versions repeat the cumulative prompt figures on message_delta.
// Taking them must not double-count, and an omitted field must not zero out
// what message_start already reported.
func TestAnthropicChatStreamPrefersTheCumulativeDeltaUsage(t *testing.T) {
	srv := anthropicServer(t, "text/event-stream", `data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":900,"cache_creation_input_tokens":50}}}

data: {"type":"message_delta","usage":{"input_tokens":100,"output_tokens":25}}

`)
	client := NewAnthropicClient(srv.URL, "claude-sonnet-4", "k", 5*time.Second)

	resp, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	}, func(string) {})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}

	want := domain.Usage{
		PromptTokens:     1050, // 100 (delta wins) + 900 + 50 (kept from message_start)
		CompletionTokens: 25,
		TotalTokens:      1075,
		CacheReadTokens:  900,
		CacheWriteTokens: 50,
	}
	if resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}
}

// OpenAI-compatible endpoints report the opposite convention: prompt_tokens
// ALREADY includes the cached share. Adding cached_tokens on top would inflate
// every cached turn's prompt.
func TestOpenAICompatUsageKeepsPromptTotalAndSplitsOutCachedTokens(t *testing.T) {
	got := usage{
		PromptTokens:        1000,
		CompletionTokens:    40,
		TotalTokens:         1040,
		PromptTokensDetails: &promptTokensDetails{CachedTokens: 800},
	}.toDomain()

	want := domain.Usage{
		PromptTokens:     1000,
		CompletionTokens: 40,
		TotalTokens:      1040,
		CacheReadTokens:  800,
		CacheWriteTokens: 0,
	}
	if got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

// A local endpoint that has never heard of prompt caching omits the details
// object entirely. That must read as "no cache", not crash and not invent one.
func TestOpenAICompatUsageWithoutDetailsReportsNoCache(t *testing.T) {
	got := usage{PromptTokens: 1000, CompletionTokens: 40, TotalTokens: 1040}.toDomain()

	want := domain.Usage{PromptTokens: 1000, CompletionTokens: 40, TotalTokens: 1040}
	if got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

// total_tokens is optional on several OpenAI-compatible servers; the derived
// fallback must survive the details object being present.
func TestOpenAICompatUsageDerivesTotalWhenAbsent(t *testing.T) {
	got := usage{
		PromptTokens:        1000,
		CompletionTokens:    40,
		PromptTokensDetails: &promptTokensDetails{CachedTokens: 800},
	}.toDomain()

	if got.TotalTokens != 1040 {
		t.Errorf("total = %d, want 1040", got.TotalTokens)
	}
	if got.CacheReadTokens != 800 {
		t.Errorf("cache read = %d, want 800", got.CacheReadTokens)
	}
}

// The streamed OpenAI-compatible path reads usage off the final choiceless
// chunk; the cache split has to survive that route too.
func TestOpenAICompatChatStreamReportsCachedTokens(t *testing.T) {
	srv, _ := streamServer(t, []string{
		`{"choices":[{"delta":{"content":"hi"}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":40,"total_tokens":1040,"prompt_tokens_details":{"cached_tokens":800}}}`,
	})
	client := NewOpenAICompatClient(srv.URL, "local-model", "", 5*time.Second)

	resp, err := client.ChatStream(context.Background(), domain.AgentRequest{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	}, func(string) {})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}

	want := domain.Usage{
		PromptTokens:     1000,
		CompletionTokens: 40,
		TotalTokens:      1040,
		CacheReadTokens:  800,
	}
	if resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}
}
