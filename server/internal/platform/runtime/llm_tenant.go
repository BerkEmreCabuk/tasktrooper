package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// providerCacheTTL bounds how long a resolved ProviderSet is reused.
//
// It exists for cost, not for correctness: resolving reads ~10 settings rows,
// decrypts an API key per provider and constructs an HTTP client for each, and
// an index pass makes thousands of embedding calls. Every one of those paying
// that would be a different kind of outage.
//
// A write through llmprovider.Service invalidates the entry at once, so the TTL
// only covers a change that reached the database some other way. It is short
// enough that a rotated key is not a support ticket.
const providerCacheTTL = 15 * time.Second

// providerCache resolves the LLM providers and keeps the one resulting set.
//
// Nothing is pushed into the MultiProviderClient: every call resolves through
// here, so a settings change reaches the next call without a reload.
type providerCache struct {
	// resolve reads the stored configuration. Injected rather than taking
	// *llmprovider.Service directly so a test can drive it with no database.
	resolve func(context.Context) (llmprovider.Resolved, error)
	// defaultTimeout is the fallback per-provider timeout, from config.yml.
	defaultTimeout time.Duration
	ttl            time.Duration
	now            func() time.Time

	mu    sync.Mutex
	entry *providerCacheEntry
}

type providerCacheEntry struct {
	set     llm.ProviderSet
	builtAt time.Time
}

func newProviderCache(resolve func(context.Context) (llmprovider.Resolved, error), defaultTimeout time.Duration) *providerCache {
	return &providerCache{
		resolve:        resolve,
		defaultTimeout: defaultTimeout,
		ttl:            providerCacheTTL,
		now:            time.Now,
	}
}

// ResolveProviders implements llm.ProviderResolver.
func (c *providerCache) ResolveProviders(ctx context.Context) (llm.ProviderSet, error) {
	if set, ok := c.cached(); ok {
		return set, nil
	}
	// Resolved OUTSIDE the lock. Holding it across a dozen database round trips
	// would serialise every LLM call behind the one that is resolving. Two
	// concurrent cold calls may both resolve and both store; the work is
	// idempotent and the second store simply wins.
	resolved, err := c.resolve(ctx)
	if err != nil {
		return llm.ProviderSet{}, err
	}
	set := c.build(resolved)
	c.mu.Lock()
	c.entry = &providerCacheEntry{set: set, builtAt: c.now()}
	c.mu.Unlock()
	return set, nil
}

func (c *providerCache) cached() (llm.ProviderSet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entry == nil {
		return llm.ProviderSet{}, false
	}
	if c.now().Sub(c.entry.builtAt) > c.ttl {
		c.entry = nil
		return llm.ProviderSet{}, false
	}
	return c.entry.set, true
}

// Invalidate drops the cached set. It is wired as llmprovider.InvalidateFunc,
// so it runs on every write that changes what Resolve would answer — which is
// what makes a credential change take effect at once rather than at the end of
// the TTL.
func (c *providerCache) Invalidate(context.Context) {
	c.mu.Lock()
	c.entry = nil
	c.mu.Unlock()
	log.Debug().Msg("llm: provider cache invalidated after a settings change")
}

// build turns the stored configuration into the clients that spend its
// credentials.
func (c *providerCache) build(r llmprovider.Resolved) llm.ProviderSet {
	set := llm.ProviderSet{
		Clients:           make(map[domain.LLMProviderType]port.LLMClient, len(r.Entries)),
		Default:           r.Default,
		EmbeddingProvider: r.EmbeddingProvider,
		EmbeddingModel:    r.EmbeddingModel,
	}
	for _, entry := range r.Entries {
		timeout := llmprovider.ResolveTimeoutDuration(entry.TimeoutSeconds, entry.ProviderType, c.defaultTimeout)
		set.Clients[entry.ProviderType] = llm.NewProviderClient(
			entry.ProviderType, entry.BaseURL, entry.DefaultModel, entry.APIKey, timeout)
	}
	return set
}
