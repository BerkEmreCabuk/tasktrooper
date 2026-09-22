package usage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeEmbedClient struct {
	mu    sync.Mutex
	calls []embedCall
}

type embedCall struct {
	input, model string
}

var _ port.LLMClient = (*fakeEmbedClient)(nil)

func (f *fakeEmbedClient) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (f *fakeEmbedClient) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (f *fakeEmbedClient) Models(context.Context) ([]string, error) { return nil, nil }

func (f *fakeEmbedClient) Embed(_ context.Context, input, model string) ([]float32, error) {
	f.mu.Lock()
	f.calls = append(f.calls, embedCall{input: input, model: model})
	f.mu.Unlock()
	return vecFor(model, input), nil
}

func (f *fakeEmbedClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func vecFor(model, input string) []float32 {
	h := sha256.Sum256([]byte(model + "\x00" + input))
	return []float32{float32(h[0]), float32(h[1]), float32(h[2]), float32(h[3])}
}

type fakeProviderClient struct {
	*fakeEmbedClient
	provider domain.LLMProviderType
}

func (f *fakeProviderClient) EmbeddingProvider(context.Context) domain.LLMProviderType {
	return f.provider
}
func (f *fakeProviderClient) Unwrap() port.LLMClient { return f.fakeEmbedClient }

func TestCachingEmbedder_HitReturnsCachedVectorWithoutCallingInner(t *testing.T) {
	inner := &fakeEmbedClient{}
	c := NewCachingEmbedder(inner, 8)

	first, err := c.Embed(context.Background(), "hello", "m1")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("call count after miss = %d, want 1", got)
	}

	second, err := c.Embed(context.Background(), "hello", "m1")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("call count after hit = %d, want still 1 (inner must not be called)", got)
	}
	if !slices.Equal(first, second) {
		t.Fatalf("cached vector = %v, want identical to %v", second, first)
	}
}

func TestCachingEmbedder_DistinctModelOrTextMisses(t *testing.T) {
	inner := &fakeEmbedClient{}
	c := NewCachingEmbedder(inner, 8)

	if _, err := c.Embed(context.Background(), "hello", "m1"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if _, err := c.Embed(context.Background(), "hello", "m2"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if _, err := c.Embed(context.Background(), "goodbye", "m1"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count = %d, want 3 (every distinct model/text pair is its own miss)", got)
	}
}

func TestCachingEmbedder_ProviderChangeMisses(t *testing.T) {
	inner := &fakeProviderClient{fakeEmbedClient: &fakeEmbedClient{}, provider: domain.LLMProviderLocal}
	c := NewCachingEmbedder(inner, 8)

	if _, err := c.Embed(context.Background(), "hello", "m1"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	inner.provider = domain.LLMProviderOpenAI
	if _, err := c.Embed(context.Background(), "hello", "m1"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 2 {
		t.Fatalf("call count = %d, want 2 (same model/text under a different provider must miss)", got)
	}
}

func TestCachingEmbedder_LRUEvictsOldestAtCapacity(t *testing.T) {
	inner := &fakeEmbedClient{}
	c := NewCachingEmbedder(inner, 2)

	for _, text := range []string{"a", "b", "c"} {
		if _, err := c.Embed(context.Background(), text, "m"); err != nil {
			t.Fatalf("embed(%q): %v", text, err)
		}
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count = %d, want 3", got)
	}

	if _, err := c.Embed(context.Background(), "b", "m"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count after re-embedding warm entry = %d, want still 3", got)
	}

	if _, err := c.Embed(context.Background(), "a", "m"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 4 {
		t.Fatalf("call count after re-embedding evicted entry = %d, want 4", got)
	}
}

func TestCachingEmbedder_DisabledBypassesCache(t *testing.T) {
	inner := &fakeEmbedClient{}
	c := NewCachingEmbedder(inner, -1)

	for range 3 {
		if _, err := c.Embed(context.Background(), "same text", "m"); err != nil {
			t.Fatalf("embed: %v", err)
		}
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count = %d, want 3 (disabled cache must never short-circuit)", got)
	}
}

func TestCachingEmbedder_BulkSequentialTextsStayBoundedByCapacity(t *testing.T) {
	inner := &fakeEmbedClient{}
	const capacity = 16
	c := NewCachingEmbedder(inner, capacity)

	for i := range capacity * 4 {
		text := fmt.Sprintf("chunk-%d", i)
		if _, err := c.Embed(context.Background(), text, "m"); err != nil {
			t.Fatalf("embed(%q): %v", text, err)
		}
	}

	c.mu.Lock()
	size := len(c.items)
	c.mu.Unlock()
	if size != capacity {
		t.Fatalf("cache size = %d, want bounded to capacity %d", size, capacity)
	}
}

func TestCachingEmbedder_ChatAndModelsPassThrough(t *testing.T) {
	inner := &fakeEmbedClient{}
	c := NewCachingEmbedder(inner, 8)

	if _, err := c.Chat(context.Background(), domain.AgentRequest{}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if _, err := c.ChatStream(context.Background(), domain.AgentRequest{}, nil); err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	if _, err := c.Models(context.Background()); err != nil {
		t.Fatalf("models: %v", err)
	}
	if c.Unwrap() != port.LLMClient(inner) {
		t.Fatalf("Unwrap() did not return the wrapped client")
	}
}
