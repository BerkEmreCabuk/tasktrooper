package usage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeEmbedClient is a minimal port.LLMClient whose Embed calls are counted
// and logged, so a test can assert whether a call actually reached the inner
// client (a cache hit must not) and what it was asked for (a cache miss must
// ask for exactly the right thing). The returned vector is deterministic in
// (model, input) so two calls for the same pair are provably "the same
// embedding" without a real provider.
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

// fakeProviderClient wraps a fakeEmbedClient and reports a mutable pinned
// embedding provider via the same EmbeddingProvider()/Unwrap() shape
// *llm.MultiProviderClient exposes, so CachingEmbedder's provider resolution
// can be exercised without importing the adapter package.
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
	if _, err := c.Embed(context.Background(), "hello", "m2"); err != nil { // different model
		t.Fatalf("embed: %v", err)
	}
	if _, err := c.Embed(context.Background(), "goodbye", "m1"); err != nil { // different text
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count = %d, want 3 (every distinct model/text pair is its own miss)", got)
	}
}

// TestCachingEmbedder_ProviderChangeMisses covers the "provider" component of
// the (provider, model, text) cache key: hot-swapping the pinned embedding
// provider (an admin action the running process supports, see
// llmprovider.Service.SetEmbedding) must not serve a vector computed by the
// old provider for the new one.
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

	for _, text := range []string{"a", "b", "c"} { // "c" overflows capacity 2, evicting "a"
		if _, err := c.Embed(context.Background(), text, "m"); err != nil {
			t.Fatalf("embed(%q): %v", text, err)
		}
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count = %d, want 3", got)
	}

	// "b" is still warm (touched more recently than "a").
	if _, err := c.Embed(context.Background(), "b", "m"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 3 {
		t.Fatalf("call count after re-embedding warm entry = %d, want still 3", got)
	}

	// "a" was evicted, so it must miss again.
	if _, err := c.Embed(context.Background(), "a", "m"); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if got := inner.callCount(); got != 4 {
		t.Fatalf("call count after re-embedding evicted entry = %d, want 4", got)
	}
}

// TestCachingEmbedder_DisabledBypassesCache covers the negative-capacity
// escape hatch (embedding.query_cache_entries < 0).
//
// port.LLMClient.Embed(ctx, input string, model string) has no batch
// parameter anywhere in this codebase — openai_compat, multi, swapping, and the
// anthropic/gemini_vertex adapters all take exactly one input string — so
// there is no "batch call" shape for the decorator to special-case; a
// negative capacity is the only bypass path that exists.
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

// TestCachingEmbedder_BulkSequentialTextsStayBoundedByCapacity stands in for
// index-time chunk embedding: many distinct one-off texts embedded back to
// back (each still a single-text call — see the package doc on cache.go).
// The LRU must bound memory rather than growing without limit, which is the
// actual risk a large initial index poses to a shared cache.
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

// The cache key's FIRST component is the tenant, and this is the reason it
// exists: two tenants asking for the same text under the same model must not
// share an entry.
//
// It reads as harmless — the same model returns the same vector — right up to
// the case that actually happens. "Auto" resolves per tenant now, so tenant A
// on OpenAI and tenant B on their own Mac's pinned model both report provider
// "", and B would be handed A's vector: a different model, a different
// dimension count, written into B's index. That is the corruption
// domain.EmbeddingProvenanceStale exists to catch after the fact.
func TestCachingEmbedder_TenantsDoNotShareCacheEntries(t *testing.T) {
	inner := &fakeEmbedClient{}
	c := NewCachingEmbedder(inner, 8)

	a := tenant.With(context.Background(), tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner})
	b := tenant.With(context.Background(), tenant.Identity{TenantID: uuid.New(), Role: tenant.RoleOwner})

	if _, err := c.Embed(a, "hello", "m1"); err != nil {
		t.Fatalf("embed for tenant A: %v", err)
	}
	if _, err := c.Embed(b, "hello", "m1"); err != nil {
		t.Fatalf("embed for tenant B: %v", err)
	}
	if got := inner.callCount(); got != 2 {
		t.Fatalf("call count = %d, want 2 — tenant B must not be served tenant A's cached vector", got)
	}

	// And a tenant still gets its own cache: the partition must not defeat the
	// point of caching at all.
	if _, err := c.Embed(a, "hello", "m1"); err != nil {
		t.Fatalf("embed for tenant A again: %v", err)
	}
	if got := inner.callCount(); got != 2 {
		t.Fatalf("call count = %d, want still 2 — a tenant's own repeat must hit", got)
	}
}
