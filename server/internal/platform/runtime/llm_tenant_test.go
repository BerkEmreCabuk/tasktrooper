package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// Tenant B must never reach tenant A's provider credential.
//
// This is the defect these tests exist for, and it was live on one replica:
// llmprovider.Service pushed the SAVING tenant's clients — API keys included —
// into one process-wide MultiProviderClient, so whoever saved LLM settings last
// decided which key every other tenant's next call spent. It was found by
// accident while reading unrelated code.
//
// The test is deliberately not a unit assertion that a field was reassigned. It
// drives the whole chain the product uses: a tenant-scoped store (the shape
// row-level security gives postgres.DB), a real cipher so the key is genuinely
// encrypted and decrypted, llmprovider.Service, the resolver and its cache, the
// MultiProviderClient, and a real HTTP round trip — so what is asserted is the
// Authorization header that actually left the process.

// tenantRows is one tenant's slice of llm_provider_configs and app_settings.
type tenantRows struct {
	configs    map[domain.LLMProviderType]domain.LLMProviderConfig
	keys       map[domain.LLMProviderType][]byte
	active     domain.LLMProviderType
	embedding  domain.LLMProviderType
	embedModel string
}

// scopedProviderStore serves each tenant only its own rows, and refuses a
// context with no tenant at all.
//
// That is exactly what postgres.DB does — every statement opens a transaction
// with SET LOCAL app.tenant_id and the policies filter on it, and a context
// with no identity gets tenant.ErrNoTenant rather than a default. Modelling it
// here is what makes the test's isolation claim mean something: if the code
// under test ever reached for another tenant's row it would have to do so
// through this store, and this store cannot answer.
type scopedProviderStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*tenantRows
}

func newScopedProviderStore() *scopedProviderStore {
	return &scopedProviderStore{rows: make(map[uuid.UUID]*tenantRows)}
}

func (s *scopedProviderStore) forCtx(ctx context.Context) (*tenantRows, error) {
	id, ok := tenant.ID(ctx)
	if !ok {
		return nil, tenant.ErrNoTenant
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		r = &tenantRows{
			configs: map[domain.LLMProviderType]domain.LLMProviderConfig{},
			keys:    map[domain.LLMProviderType][]byte{},
			active:  domain.LLMProviderLocal,
		}
		s.rows[id] = r
	}
	return r, nil
}

func (s *scopedProviderStore) List(ctx context.Context) ([]domain.LLMProviderConfig, error) {
	r, err := s.forCtx(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.LLMProviderConfig, 0, len(r.configs))
	for _, c := range r.configs {
		out = append(out, c)
	}
	return out, nil
}

func (s *scopedProviderStore) Get(ctx context.Context, pt domain.LLMProviderType) (domain.LLMProviderConfig, error) {
	r, err := s.forCtx(ctx)
	if err != nil {
		return domain.LLMProviderConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return r.configs[pt], nil
}

func (s *scopedProviderStore) Upsert(ctx context.Context, cfg domain.LLMProviderConfig) error {
	r, err := s.forCtx(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.configs[cfg.ProviderType] = cfg
	return nil
}

func (s *scopedProviderStore) SetAPIKey(ctx context.Context, pt domain.LLMProviderType, enc []byte) error {
	r, err := s.forCtx(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.keys[pt] = enc
	return nil
}

func (s *scopedProviderStore) GetAPIKeyEncrypted(ctx context.Context, pt domain.LLMProviderType) ([]byte, error) {
	r, err := s.forCtx(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return r.keys[pt], nil
}

func (s *scopedProviderStore) DeleteAPIKey(ctx context.Context, pt domain.LLMProviderType) error {
	r, err := s.forCtx(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(r.keys, pt)
	return nil
}

func (s *scopedProviderStore) GetActiveProvider(ctx context.Context) (domain.LLMProviderType, error) {
	r, err := s.forCtx(ctx)
	if err != nil {
		return "", err
	}
	return r.active, nil
}

func (s *scopedProviderStore) SetActiveProvider(ctx context.Context, pt domain.LLMProviderType) error {
	r, err := s.forCtx(ctx)
	if err != nil {
		return err
	}
	r.active = pt
	return nil
}

func (s *scopedProviderStore) GetEmbeddingProvider(ctx context.Context) (domain.LLMProviderType, error) {
	r, err := s.forCtx(ctx)
	if err != nil {
		return "", err
	}
	return r.embedding, nil
}

func (s *scopedProviderStore) SetEmbeddingProvider(ctx context.Context, pt domain.LLMProviderType) error {
	r, err := s.forCtx(ctx)
	if err != nil {
		return err
	}
	r.embedding = pt
	return nil
}

func (s *scopedProviderStore) GetEmbeddingModel(ctx context.Context) (string, error) {
	r, err := s.forCtx(ctx)
	if err != nil {
		return "", err
	}
	return r.embedModel, nil
}

func (s *scopedProviderStore) SetEmbeddingModel(ctx context.Context, model string) error {
	r, err := s.forCtx(ctx)
	if err != nil {
		return err
	}
	r.embedModel = model
	return nil
}

// keyRecorder is a stand-in for a provider's API. It records the bearer token
// each request presented, which is the thing under test: a credential that
// reached the wire is a credential that was spent.
type keyRecorder struct {
	mu   sync.Mutex
	seen []string
	srv  *httptest.Server
}

func newKeyRecorder(t *testing.T) *keyRecorder {
	t.Helper()
	k := &keyRecorder{}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		k.seen = append(k.seen, r.Header.Get("Authorization"))
		k.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *keyRecorder) keys() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.seen...)
}

type leakFixture struct {
	store  *scopedProviderStore
	svc    *llmprovider.Service
	client *llm.MultiProviderClient
	cache  *tenantProviders
}

func newLeakFixture(t *testing.T) *leakFixture {
	t.Helper()
	cipher := testCipherFor(t)
	store := newScopedProviderStore()

	f := &leakFixture{store: store}
	// The same construction order buildHandler uses: the cache is built first
	// so the service can be given its invalidation hook, then installed on the
	// client.
	f.cache = newTenantProviders(func(ctx context.Context) (llmprovider.Resolved, error) {
		return f.svc.Resolve(ctx)
	}, 30*time.Second)
	f.svc = llmprovider.NewService(store, nil, cipher, 30*time.Second, f.cache.Invalidate)
	f.client = llm.NewMultiProviderClient(nil, f.cache)
	return f
}

func ctxFor(id uuid.UUID) context.Context {
	return tenant.With(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleOwner})
}

// connect saves a provider for one tenant, writing straight to the store and
// then invalidating exactly as Service.Connect does. Connect itself probes the
// provider over the network before saving, which is not what this test is
// about.
func (f *leakFixture) connect(t *testing.T, ctx context.Context, baseURL, apiKey string) {
	t.Helper()
	cfg := domain.LLMProviderConfig{
		ProviderType:   domain.LLMProviderOpenAI,
		BaseURL:        baseURL,
		DefaultModel:   "gpt-4o",
		TimeoutSeconds: 30,
		Configured:     true,
	}
	if err := f.store.Upsert(ctx, cfg); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	enc, err := secretsEncrypt(t, apiKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := f.store.SetAPIKey(ctx, domain.LLMProviderOpenAI, enc); err != nil {
		t.Fatalf("set key: %v", err)
	}
	if err := f.store.SetActiveProvider(ctx, domain.LLMProviderOpenAI); err != nil {
		t.Fatalf("set active: %v", err)
	}
	f.cache.Invalidate(ctx)
}

// testCipherFor is the one cipher the fixture and its writes share, so a key
// encrypted by the test decrypts inside the service exactly as a real one does.
var (
	testCipherOnce sync.Once
	testCipher     *secrets.Cipher
)

func testCipherFor(t *testing.T) *secrets.Cipher {
	t.Helper()
	testCipherOnce.Do(func() {
		c, err := secrets.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("cipher: %v", err)
		}
		testCipher = c
	})
	return testCipher
}

func secretsEncrypt(t *testing.T, plain string) ([]byte, error) {
	t.Helper()
	return testCipherFor(t).Encrypt(plain)
}

func chat(t *testing.T, c *llm.MultiProviderClient, ctx context.Context) {
	t.Helper()
	_, err := c.Chat(ctx, domain.AgentRequest{
		Model:    "gpt-4o",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
}

// The defect, stated. Tenant A connects a provider; tenant B connects their own
// afterwards; A calls. Before this change the process held one client map and B
// had overwritten it, so A's call went to B's endpoint with B's key.
func TestTenantNeverReachesAnotherTenantsCredential(t *testing.T) {
	f := newLeakFixture(t)
	serverA, serverB := newKeyRecorder(t), newKeyRecorder(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	ctxA, ctxB := ctxFor(tenantA), ctxFor(tenantB)

	f.connect(t, ctxA, serverA.srv.URL, "sk-tenant-a")
	// B saves LAST, which is the ordering that used to decide the whole
	// process's credentials.
	f.connect(t, ctxB, serverB.srv.URL, "sk-tenant-b")

	chat(t, f.client, ctxA)

	if got := serverA.keys(); len(got) != 1 || got[0] != "Bearer sk-tenant-a" {
		t.Fatalf("tenant A's own endpoint saw %v, want one call with A's key", got)
	}
	if got := serverB.keys(); len(got) != 0 {
		t.Fatalf("tenant A's call reached tenant B's endpoint: %v", got)
	}

	chat(t, f.client, ctxB)
	if got := serverB.keys(); len(got) != 1 || got[0] != "Bearer sk-tenant-b" {
		t.Fatalf("tenant B's endpoint saw %v, want one call with B's key", got)
	}
	if got := serverA.keys(); len(got) != 1 {
		t.Fatalf("tenant B's call also reached tenant A's endpoint: %v", got)
	}
}

// Interleaved, with no save between the calls — which is the shape that
// actually exposes a shared cache.
//
// The version of this test that put a connect before every chat passed against
// a deliberately broken cache key, and the reason is worth recording: a connect
// invalidates, so the next call always resolved fresh, and resolution reads the
// caller's own context and was never the broken half. The bug lives in REUSE.
// So both tenants connect once, and then simply take turns — A's call warms an
// entry, B's call must not be served it.
func TestInterleavedCallsFromTwoTenantsStayWithTheirOwnCredential(t *testing.T) {
	f := newLeakFixture(t)
	serverA, serverB := newKeyRecorder(t), newKeyRecorder(t)
	ctxA, ctxB := ctxFor(uuid.New()), ctxFor(uuid.New())

	f.connect(t, ctxA, serverA.srv.URL, "sk-tenant-a")
	f.connect(t, ctxB, serverB.srv.URL, "sk-tenant-b")

	for i := 0; i < 3; i++ {
		chat(t, f.client, ctxA)
		chat(t, f.client, ctxB)
	}

	// Both halves are asserted, and both are needed. The KEY check catches a
	// credential crossing over; the COUNT check catches the call itself being
	// routed to the wrong tenant's endpoint — which the key check alone misses,
	// because a call carrying A's key to A's endpoint looks correct however it
	// got there.
	for _, k := range serverA.keys() {
		if k != "Bearer sk-tenant-a" {
			t.Fatalf("tenant A's endpoint was presented %q", k)
		}
	}
	for _, k := range serverB.keys() {
		if k != "Bearer sk-tenant-b" {
			t.Fatalf("tenant B's endpoint was presented %q", k)
		}
	}
	if len(serverA.keys()) != 3 || len(serverB.keys()) != 3 {
		t.Fatalf("call counts A=%d B=%d, want 3 each — a call reached the wrong tenant's provider",
			len(serverA.keys()), len(serverB.keys()))
	}
}

// Concurrently, because a cache is where a race would hide: two tenants calling
// at the same moment must each resolve their own set, whichever wins the map.
func TestConcurrentCallsFromTwoTenantsDoNotCross(t *testing.T) {
	f := newLeakFixture(t)
	serverA, serverB := newKeyRecorder(t), newKeyRecorder(t)
	ctxA, ctxB := ctxFor(uuid.New()), ctxFor(uuid.New())
	f.connect(t, ctxA, serverA.srv.URL, "sk-tenant-a")
	f.connect(t, ctxB, serverB.srv.URL, "sk-tenant-b")

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); <-start; chat(t, f.client, ctxA) }()
		go func() { defer wg.Done(); <-start; chat(t, f.client, ctxB) }()
	}
	close(start)
	wg.Wait()

	for _, k := range serverA.keys() {
		if k != "Bearer sk-tenant-a" {
			t.Fatalf("tenant A's endpoint was presented %q under concurrency", k)
		}
	}
	for _, k := range serverB.keys() {
		if k != "Bearer sk-tenant-b" {
			t.Fatalf("tenant B's endpoint was presented %q under concurrency", k)
		}
	}
	if len(serverA.keys()) != 8 || len(serverB.keys()) != 8 {
		t.Fatalf("call counts A=%d B=%d, want 8 each", len(serverA.keys()), len(serverB.keys()))
	}
}

// A rotated key takes effect at once on the replica that served the rotation —
// that is what the invalidation hook is for. Without it the tenant would keep
// spending the old credential until the TTL expired.
func TestRotatingAKeyTakesEffectImmediately(t *testing.T) {
	f := newLeakFixture(t)
	server := newKeyRecorder(t)
	ctxA := ctxFor(uuid.New())

	f.connect(t, ctxA, server.srv.URL, "sk-old")
	chat(t, f.client, ctxA)
	f.connect(t, ctxA, server.srv.URL, "sk-new")
	chat(t, f.client, ctxA)

	got := server.keys()
	if len(got) != 2 || got[0] != "Bearer sk-old" || got[1] != "Bearer sk-new" {
		t.Fatalf("keys presented = %v, want the old one then the new one", got)
	}
}

// The cache is keyed by tenant and bounded by a TTL, so a change made on
// ANOTHER replica — which cannot invalidate this one — is picked up when the
// entry ages out. The window is same-tenant staleness, never a cross-tenant
// read: this asserts the entry expires at all.
func TestCacheEntryExpires(t *testing.T) {
	f := newLeakFixture(t)
	server := newKeyRecorder(t)
	ctxA := ctxFor(uuid.New())
	f.connect(t, ctxA, server.srv.URL, "sk-old")
	chat(t, f.client, ctxA)

	// A write that this replica never saw: the store changes underneath, with
	// no invalidation.
	enc, err := secretsEncrypt(t, "sk-rotated-elsewhere")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := f.store.SetAPIKey(ctxA, domain.LLMProviderOpenAI, enc); err != nil {
		t.Fatalf("set key: %v", err)
	}

	chat(t, f.client, ctxA)
	if got := server.keys(); got[1] != "Bearer sk-old" {
		t.Fatalf("inside the TTL the cached credential should still be used, got %q", got[1])
	}

	// Age the entry past the TTL the way time would.
	f.cache.now = func() time.Time { return time.Now().Add(2 * providerCacheTTL) }
	chat(t, f.client, ctxA)
	if got := server.keys(); got[2] != "Bearer sk-rotated-elsewhere" {
		t.Fatalf("after the TTL the new credential must be picked up, got %q", got[2])
	}
}

// A context with no tenant resolves nothing rather than falling back to some
// tenant's providers. The single-tenant deployments that legitimately have no
// identity on a background context still work, because they reach the
// config.yml fallback — never another tenant's client.
func TestNoTenantResolvesNothing(t *testing.T) {
	f := newLeakFixture(t)
	server := newKeyRecorder(t)
	f.connect(t, ctxFor(uuid.New()), server.srv.URL, "sk-tenant-a")

	_, err := f.client.Chat(context.Background(), domain.AgentRequest{
		Model:    "gpt-4o",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("a call with no tenant must not be served by some tenant's client")
	}
	if got := server.keys(); len(got) != 0 {
		t.Fatalf("a tenant's endpoint was reached with no tenant on the context: %v", got)
	}
}
