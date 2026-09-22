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

func NewCachingEmbedder(inner port.LLMClient, capacity int) *CachingEmbedder {
	return &CachingEmbedder{
		inner:    inner,
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

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
