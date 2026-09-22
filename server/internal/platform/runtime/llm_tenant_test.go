package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// These tests drive the whole chain (store, real cipher, service, cache,
// MultiProviderClient, a real HTTP round trip), so what is asserted is the
// Authorization header that actually left the process.

// memProviderStore is llm_provider_configs and app_settings, in memory.
type memProviderStore struct {
	mu         sync.Mutex
	configs    map[domain.LLMProviderType]domain.LLMProviderConfig
	keys       map[domain.LLMProviderType][]byte
	active     domain.LLMProviderType
	embedding  domain.LLMProviderType
	embedModel string
}

func newMemProviderStore() *memProviderStore {
	return &memProviderStore{
		configs: map[domain.LLMProviderType]domain.LLMProviderConfig{},
		keys:    map[domain.LLMProviderType][]byte{},
		active:  domain.LLMProviderLocal,
	}
}

func (s *memProviderStore) List(context.Context) ([]domain.LLMProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.LLMProviderConfig, 0, len(s.configs))
	for _, c := range s.configs {
		out = append(out, c)
	}
	return out, nil
}

func (s *memProviderStore) Get(_ context.Context, pt domain.LLMProviderType) (domain.LLMProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configs[pt], nil
}

func (s *memProviderStore) Upsert(_ context.Context, cfg domain.LLMProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[cfg.ProviderType] = cfg
	return nil
}

func (s *memProviderStore) SetAPIKey(_ context.Context, pt domain.LLMProviderType, enc []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[pt] = enc
	return nil
}

func (s *memProviderStore) GetAPIKeyEncrypted(_ context.Context, pt domain.LLMProviderType) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[pt], nil
}

func (s *memProviderStore) DeleteAPIKey(_ context.Context, pt domain.LLMProviderType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, pt)
	return nil
}

func (s *memProviderStore) GetActiveProvider(context.Context) (domain.LLMProviderType, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, nil
}

func (s *memProviderStore) SetActiveProvider(_ context.Context, pt domain.LLMProviderType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = pt
	return nil
}

func (s *memProviderStore) GetEmbeddingProvider(context.Context) (domain.LLMProviderType, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.embedding, nil
}

func (s *memProviderStore) SetEmbeddingProvider(_ context.Context, pt domain.LLMProviderType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedding = pt
	return nil
}

func (s *memProviderStore) GetEmbeddingModel(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.embedModel, nil
}

func (s *memProviderStore) SetEmbeddingModel(_ context.Context, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedModel = model
	return nil
}

// keyRecorder records the bearer token each request presented — a credential
// that reached the wire is a credential that was spent.
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

type providerFixture struct {
	store  *memProviderStore
	svc    *llmprovider.Service
	client *llm.MultiProviderClient
	cache  *providerCache
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	cipher := testCipherFor(t)
	store := newMemProviderStore()

	f := &providerFixture{store: store}
	// The same construction order buildHandler uses: the cache is built first
	// so the service can receive its invalidation hook, then installed.
	f.cache = newProviderCache(func(ctx context.Context) (llmprovider.Resolved, error) {
		return f.svc.Resolve(ctx)
	}, 30*time.Second)
	f.svc = llmprovider.NewService(store, nil, cipher, 30*time.Second, f.cache.Invalidate)
	f.client = llm.NewMultiProviderClient(nil, f.cache)
	return f
}

// connect saves a provider, bypassing Service.Connect's network probe, writing
// straight to the store and invalidating exactly as Connect does.
func (f *providerFixture) connect(t *testing.T, ctx context.Context, baseURL, apiKey string) {
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

// A rotated key takes effect at once — that is what the invalidation hook is
// for; otherwise the next calls would keep spending the old credential.
func TestRotatingAKeyTakesEffectImmediately(t *testing.T) {
	f := newProviderFixture(t)
	server := newKeyRecorder(t)
	ctx := context.Background()

	f.connect(t, ctx, server.srv.URL, "sk-old")
	chat(t, f.client, ctx)
	f.connect(t, ctx, server.srv.URL, "sk-new")
	chat(t, f.client, ctx)

	got := server.keys()
	if len(got) != 2 || got[0] != "Bearer sk-old" || got[1] != "Bearer sk-new" {
		t.Fatalf("keys presented = %v, want the old one then the new one", got)
	}
}

// A change that reached the database without the service (which therefore
// cannot invalidate) is picked up when the entry ages out.
func TestCacheEntryExpires(t *testing.T) {
	f := newProviderFixture(t)
	server := newKeyRecorder(t)
	ctx := context.Background()
	f.connect(t, ctx, server.srv.URL, "sk-old")
	chat(t, f.client, ctx)

	// The store changes underneath, with no invalidation.
	enc, err := secretsEncrypt(t, "sk-rotated-elsewhere")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := f.store.SetAPIKey(ctx, domain.LLMProviderOpenAI, enc); err != nil {
		t.Fatalf("set key: %v", err)
	}

	chat(t, f.client, ctx)
	if got := server.keys(); got[1] != "Bearer sk-old" {
		t.Fatalf("inside the TTL the cached credential should still be used, got %q", got[1])
	}

	// Age the entry past the TTL the way time would.
	f.cache.now = func() time.Time { return time.Now().Add(2 * providerCacheTTL) }
	chat(t, f.client, ctx)
	if got := server.keys(); got[2] != "Bearer sk-rotated-elsewhere" {
		t.Fatalf("after the TTL the new credential must be picked up, got %q", got[2])
	}
}
