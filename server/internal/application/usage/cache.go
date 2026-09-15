package usage

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// CachingEmbedder decorates an LLM client with a bounded in-memory LRU of
// query embedding vectors, keyed by a SHA-256 hash of (embedding provider,
// model, exact input text). The same text gets embedded repeatedly across a
// chat/board run — a rewritten codebase_search query, the planner's
// SearchSkills call on every plan, memory recall — and each call recomputed
// the vector from scratch even though an identical call had just been made.
//
// port.LLMClient.Embed takes exactly one input string; there is no batch
// variant anywhere in this codebase (openai_compat, multi, swapping, and the
// anthropic/gemini_vertex adapters all implement the same single-text
// signature). So every call — including the index-time chunk embedding in
// indexer/service.go's finalizeIndex — is already a "one text" request and is
// eligible for caching; there is no "batch size" to special-case. Index-time
// embedding does not thrash this cache in practice either:
// indexer/incremental.go's SHA-256 content dedup means a chunk is only
// re-embedded when its content changed, so a steady-state (re)index adds new
// entries once, not on every pass — and the LRU bound below still caps the
// damage a large initial index could otherwise do.
//
// Wired outside RecordingClient in runtime.go: a cache hit never reaches the
// recorder, so nothing is billed for a call that spent nothing.
type CachingEmbedder struct {
	inner port.LLMClient

	mu       sync.Mutex
	capacity int
	ll       *list.List
	items    map[string]*list.Element
}

var _ port.LLMClient = (*CachingEmbedder)(nil)

type embedCacheEntry struct {
	key   string
	value []float32
}

// NewCachingEmbedder wraps inner with a query-vector LRU bounded to capacity
// entries. capacity <= 0 disables caching: Embed always delegates to inner.
func NewCachingEmbedder(inner port.LLMClient, capacity int) *CachingEmbedder {
	return &CachingEmbedder{
		inner:    inner,
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

// Unwrap exposes the wrapped client — see RecordingClient.Unwrap.
func (c *CachingEmbedder) Unwrap() port.LLMClient { return c.inner }

func (c *CachingEmbedder) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	return c.inner.Chat(ctx, req)
}

func (c *CachingEmbedder) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	return c.inner.ChatStream(ctx, req, onToken)
}

func (c *CachingEmbedder) Models(ctx context.Context) ([]string, error) {
	return c.inner.Models(ctx)
}

func (c *CachingEmbedder) Embed(ctx context.Context, input, model string) ([]float32, error) {
	if c.capacity <= 0 {
		return c.inner.Embed(ctx, input, model)
	}
	key := embedCacheKey(c.embeddingProvider(ctx), model, input)
	if vec, ok := c.get(key); ok {
		return vec, nil
	}
	vec, err := c.inner.Embed(ctx, input, model)
	if err != nil {
		return nil, err
	}
	c.put(key, vec)
	return vec, nil
}

// embeddingProvider resolves the provider the wrapped client is pinned to, when
// one is reachable, by walking the same Unwrap() chain
// resolveMultiClient uses in adapter/http/handler.go (duck-typed against the
// method, not the concrete *llm.MultiProviderClient type, so this package
// does not need to import the adapter layer). "" is a valid outcome — auto
// mode, or no reachable reporter.
//
// It takes a context because the pin is a stored setting, read through the
// store like any other.
func (c *CachingEmbedder) embeddingProvider(ctx context.Context) string {
	var cur port.LLMClient = c.inner
	for i := 0; i < 8 && cur != nil; i++ {
		if r, ok := cur.(interface {
			EmbeddingProvider(context.Context) domain.LLMProviderType
		}); ok {
			return string(r.EmbeddingProvider(ctx))
		}
		u, ok := cur.(interface{ Unwrap() port.LLMClient })
		if !ok {
			return ""
		}
		cur = u.Unwrap()
	}
	return ""
}

// embedCacheKey keys an entry by provider, model and text, each separated so
// "ab"+"c" and "a"+"bc" cannot collide. The provider is part of it because two
// models return vectors that are not comparable, even for the same text.
func embedCacheKey(provider, model, input string) string {
	h := sha256.New()
	h.Write([]byte(provider))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *CachingEmbedder) get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*embedCacheEntry).value, true
}

func (c *CachingEmbedder) put(key string, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*embedCacheEntry).value = vec
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&embedCacheEntry{key: key, value: vec})
	for c.ll.Len() > c.capacity {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		c.ll.Remove(oldest)
		delete(c.items, oldest.Value.(*embedCacheEntry).key)
	}
}
