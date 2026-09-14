package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// providerCacheTTL bounds how long a resolved ProviderSet is reused.
//
// It exists for cost, not for correctness: resolving reads ~10 policy-scoped
// rows, decrypts an API key per provider and constructs an HTTP client for
// each, and an index pass makes thousands of embedding calls. Every one of
// those paying that would be a different kind of outage.
//
// Fifteen seconds, and the number is the answer to one question: how long may a
// tenant's OWN credential change take to reach a replica that did not serve the
// change? On the replica that did serve it the answer is "immediately" —
// llmprovider.Service invalidates that tenant's entry on every write. This TTL
// covers only the other replicas, which have no way to be told. It is
// deliberately short enough that a rotated key is not a support ticket.
//
// What it is NOT is a window on the bug this cache replaced. A stale entry
// serves a tenant their own previous credentials; it can never serve them
// somebody else's, because the entry is keyed by tenant and built from that
// tenant's rows alone.
const providerCacheTTL = 15 * time.Second

// tenantProviders resolves each tenant's LLM providers, and caches the result
// per tenant.
//
// This is the replacement for engine.reloadProviders, and the difference is the
// whole point of the change. reloadProviders was called from a request path and
// wrote ONE process-wide client map, so the last tenant to save LLM settings
// decided which clients — and therefore whose API keys — every other tenant
// used next. Here nothing is written to a shared location at all: a call
// resolves the tenant on its own context, and the only shared structure is a
// map keyed BY tenant, which one tenant cannot read another's entry out of.
type tenantProviders struct {
	// resolve reads one tenant's configuration. Injected rather than taking
	// *llmprovider.Service directly so the cross-tenant test can drive this
	// with two tenants' rows and no database.
	resolve func(context.Context) (llmprovider.Resolved, error)
	// defaultTimeout is the fallback per-provider timeout, from config.yml.
	defaultTimeout time.Duration
	ttl            time.Duration
	now            func() time.Time

	mu      sync.Mutex
	entries map[uuid.UUID]providerCacheEntry
}

type providerCacheEntry struct {
	set     llm.ProviderSet
	builtAt time.Time
}

func newTenantProviders(resolve func(context.Context) (llmprovider.Resolved, error), defaultTimeout time.Duration) *tenantProviders {
	return &tenantProviders{
		resolve:        resolve,
		defaultTimeout: defaultTimeout,
		ttl:            providerCacheTTL,
		now:            time.Now,
		entries:        make(map[uuid.UUID]providerCacheEntry),
	}
}

// cacheKey is the tenant on ctx, or the zero uuid when there is none.
//
// The zero uuid is the single-tenant case — a self-hosted install before the
// middleware has run, a test — and it is a real key rather than a bypass: those
// deployments have one tenant, so one entry is exactly right and they pay one
// resolve per TTL window rather than one per call. It can never collide with a
// real tenant, because tenant.From rejects uuid.Nil as an identity.
func cacheKey(ctx context.Context) uuid.UUID {
	if id, ok := tenant.ID(ctx); ok {
		return id
	}
	return uuid.Nil
}

// ResolveProviders implements llm.ProviderResolver.
func (t *tenantProviders) ResolveProviders(ctx context.Context) (llm.ProviderSet, error) {
	key := cacheKey(ctx)
	if set, ok := t.cached(key); ok {
		return set, nil
	}
	// Resolved OUTSIDE the lock. Holding it across a dozen database round trips
	// would serialise every tenant's LLM calls behind whichever tenant happened
	// to be resolving. Two concurrent calls for the same cold tenant may both
	// resolve and both store; the work is idempotent and the second store
	// simply wins.
	resolved, err := t.resolve(ctx)
	if err != nil {
		return llm.ProviderSet{}, err
	}
	set := t.build(resolved)
	t.mu.Lock()
	t.entries[key] = providerCacheEntry{set: set, builtAt: t.now()}
	t.mu.Unlock()
	return set, nil
}

func (t *tenantProviders) cached(key uuid.UUID) (llm.ProviderSet, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[key]
	if !ok {
		return llm.ProviderSet{}, false
	}
	if t.now().Sub(entry.builtAt) > t.ttl {
		delete(t.entries, key)
		return llm.ProviderSet{}, false
	}
	return entry.set, true
}

// Invalidate drops the acting tenant's entry. It is wired as
// llmprovider.InvalidateFunc, so it runs on every write that changes what
// Resolve would answer — which is what makes a credential change take effect on
// this replica at once rather than at the end of the TTL.
func (t *tenantProviders) Invalidate(ctx context.Context) {
	key := cacheKey(ctx)
	t.mu.Lock()
	delete(t.entries, key)
	t.mu.Unlock()
	log.Debug().Str("tenant_id", key.String()).Msg("llm: provider cache invalidated after a settings change")
}

// build turns one tenant's configuration into the clients that spend their
// credentials. It is the body of the old engine.reloadProviders, minus the part
// that wrote them somewhere every tenant could reach.
func (t *tenantProviders) build(r llmprovider.Resolved) llm.ProviderSet {
	set := llm.ProviderSet{
		Clients:           make(map[domain.LLMProviderType]port.LLMClient, len(r.Entries)),
		Default:           r.Default,
		EmbeddingProvider: r.EmbeddingProvider,
		EmbeddingModel:    r.EmbeddingModel,
	}
	for _, entry := range r.Entries {
		timeout := llmprovider.ResolveTimeoutDuration(entry.TimeoutSeconds, entry.ProviderType, t.defaultTimeout)
		set.Clients[entry.ProviderType] = llm.NewProviderClient(
			entry.ProviderType, entry.BaseURL, entry.DefaultModel, entry.APIKey, timeout)
	}
	return set
}
