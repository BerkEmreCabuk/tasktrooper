package usage

import (
	"context"
	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeUsageStore is a minimal port.LLMUsageStore that records every call it
// receives, so a test can assert both whether a record landed and what it
// contained.
type fakeUsageStore struct {
	mu      sync.Mutex
	records []domain.LLMUsageRecord
}

func (f *fakeUsageStore) Record(_ context.Context, rec domain.LLMUsageRecord) error {
	f.mu.Lock()
	f.records = append(f.records, rec)
	f.mu.Unlock()
	return nil
}

func (f *fakeUsageStore) Summary(context.Context, int) (domain.LLMUsageSummary, error) {
	return domain.LLMUsageSummary{}, nil
}

func (f *fakeUsageStore) snapshot() []domain.LLMUsageRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.LLMUsageRecord, len(f.records))
	copy(out, f.records)
	return out
}

// TestRecordingClient_EmbedRecordsEstimatedPromptTokens: Embed's response
// carries no usage struct (see the TODO on RecordingClient.Embed), so the
// recorder estimates prompt tokens with the same chars-per-token heuristic
// the context budget uses, and leaves completion/cache fields at zero — there
// is nothing else it could report for an embedding call.
func TestRecordingClient_EmbedRecordsEstimatedPromptTokens(t *testing.T) {
	inner := &fakeEmbedClient{}
	store := &fakeUsageStore{}
	rec := NewRecordingClient(inner, store)

	longQuery := "how does the board dispatcher recover an orphaned task run"
	_, err := rec.Embed(context.Background(), longQuery, "m1")
	require.NoError(t, err)

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	got := store.snapshot()[0]
	require.Equal(t, "m1", got.Model)
	require.Positive(t, got.PromptTokens, "prompt_tokens should be a positive estimate")
	require.Zero(t, got.CompletionTokens)
	require.Zero(t, got.CacheReadTokens)
	require.Zero(t, got.CacheWriteTokens)
}

// TestRecordingClient_EmbedDefaultsEmptyModel matches the "(default)" naming
// RecordingClient.record already applies to Chat/ChatStream when the caller
// leaves the model unset.
func TestRecordingClient_EmbedDefaultsEmptyModel(t *testing.T) {
	inner := &fakeEmbedClient{}
	store := &fakeUsageStore{}
	rec := NewRecordingClient(inner, store)

	_, err := rec.Embed(context.Background(), "text", "")
	require.NoError(t, err)

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, "(default)", store.snapshot()[0].Model)
}

// TestRecordingClient_EmbedSkipsRecordingOnError mirrors Chat/ChatStream: a
// failed call spent nothing billable and must not produce a usage row.
func TestRecordingClient_EmbedSkipsRecordingOnError(t *testing.T) {
	inner := &erroringEmbedClient{}
	store := &fakeUsageStore{}
	rec := NewRecordingClient(inner, store)

	_, err := rec.Embed(context.Background(), "text", "m1")
	require.Error(t, err)

	time.Sleep(20 * time.Millisecond)
	require.Empty(t, store.snapshot())
}

type erroringEmbedClient struct{ fakeEmbedClient }

func (e *erroringEmbedClient) Embed(context.Context, string, string) ([]float32, error) {
	return nil, context.DeadlineExceeded
}

// TestCachingEmbedder_HitsSkipRecordingMissesRecord is the wire-order
// contract from runtime.go: CachingEmbedder sits outside RecordingClient, so
// a cache hit returns before RecordingClient.Embed ever runs and a miss
// records exactly once.
func TestCachingEmbedder_HitsSkipRecordingMissesRecord(t *testing.T) {
	inner := &fakeEmbedClient{}
	store := &fakeUsageStore{}
	rec := NewRecordingClient(inner, store)
	cache := NewCachingEmbedder(rec, 8)

	if _, err := cache.Embed(context.Background(), "q", "m1"); err != nil { // miss
		t.Fatalf("embed: %v", err)
	}
	if _, err := cache.Embed(context.Background(), "q", "m1"); err != nil { // hit
		t.Fatalf("embed: %v", err)
	}

	require.Eventually(t, func() bool { return len(store.snapshot()) >= 1 }, time.Second, 5*time.Millisecond)
	// The hit path never calls RecordingClient.Embed at all (no goroutine is
	// even spawned for it), so this window is just extra insurance against a
	// wiring regression, not a real race to wait out.
	time.Sleep(20 * time.Millisecond)
	require.Len(t, store.snapshot(), 1, "a cache hit must not record usage")
	require.Equal(t, 1, inner.callCount(), "a cache hit must not reach the provider")
}

// The two confirmations that matter, and they concern money.
//
// record() used to detach onto context.Background(), so every chat and every
// embedding call wrote its tokens with no tenant on the context — which the
// store answers with tenant.ErrNoTenant. Nothing accrued, so the budget gate
// (billing.Service.Allow) compared spend against a period that was always
// empty and never tripped, and no tenant was ever billed. One Warn line per
// call, in a goroutine, was the whole signal.

// tenantUsageStore records what tenant each write arrived under, refusing an
// unscoped one exactly as the postgres store does.
type tenantUsageStore struct {
	mu      sync.Mutex
	byTenat map[uuid.UUID][]domain.LLMUsageRecord
	unscope int
}

func newTenantUsageStore() *tenantUsageStore {
	return &tenantUsageStore{byTenat: map[uuid.UUID][]domain.LLMUsageRecord{}}
}

func (s *tenantUsageStore) Record(ctx context.Context, rec domain.LLMUsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := tenant.ID(ctx)
	if !ok {
		s.unscope++
		return tenant.ErrNoTenant
	}
	s.byTenat[id] = append(s.byTenat[id], rec)
	return nil
}

func (s *tenantUsageStore) Summary(context.Context, int) (domain.LLMUsageSummary, error) {
	return domain.LLMUsageSummary{}, nil
}

func (s *tenantUsageStore) recorded(id uuid.UUID) []domain.LLMUsageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.LLMUsageRecord(nil), s.byTenat[id]...)
}

func (s *tenantUsageStore) unscopedWrites() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unscope
}

// usageInner answers both a chat and an embedding with a fixed usage.
type usageInner struct{}

func (usageInner) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{Usage: domain.Usage{PromptTokens: 11, CompletionTokens: 7}}, nil
}
func (usageInner) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{Usage: domain.Usage{PromptTokens: 5, CompletionTokens: 3}}, nil
}
func (usageInner) Models(context.Context) ([]string, error) { return nil, nil }
func (usageInner) Embed(context.Context, string, string) ([]float32, error) {
	return []float32{1, 2, 3}, nil
}

func waitForRecords(t *testing.T, store *tenantUsageStore, id uuid.UUID, want int) []domain.LLMUsageRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := store.recorded(id); len(got) >= want {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("only %d usage records for %s, want %d", len(store.recorded(id)), id, want)
	return nil
}

func TestChatAndEmbedUsageRecordAgainstTheCallingTenant(t *testing.T) {
	store := newTenantUsageStore()
	client := NewRecordingClient(usageInner{}, store)

	id := uuid.New()
	ctx := tenant.With(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleOwner})

	if _, err := client.Chat(ctx, domain.AgentRequest{Model: "gpt-4o"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if _, err := client.Embed(ctx, "some text", "text-embedding-3-small"); err != nil {
		t.Fatalf("embed: %v", err)
	}

	got := waitForRecords(t, store, id, 2)
	if store.unscopedWrites() != 0 {
		t.Fatalf("%d usage writes arrived with no tenant — they would be dropped in production",
			store.unscopedWrites())
	}

	var sawChat, sawEmbed bool
	for _, rec := range got {
		switch rec.Model {
		case "gpt-4o":
			sawChat = true
			if rec.PromptTokens != 11 || rec.CompletionTokens != 7 {
				t.Fatalf("chat usage = %d/%d, want 11/7", rec.PromptTokens, rec.CompletionTokens)
			}
		case "text-embedding-3-small":
			sawEmbed = true
			if rec.PromptTokens == 0 {
				t.Fatal("an embedding call must accrue prompt tokens")
			}
		}
	}
	if !sawChat || !sawEmbed {
		t.Fatalf("recorded %+v, want one chat and one embedding record", got)
	}
}
