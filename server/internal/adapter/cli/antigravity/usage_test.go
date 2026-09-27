package antigravity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeUsageStore struct {
	mu      sync.Mutex
	records []domain.LLMUsageRecord
}

func (f *fakeUsageStore) Record(_ context.Context, rec domain.LLMUsageRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, rec)
	return nil
}

func (f *fakeUsageStore) Summary(context.Context, time.Time, *time.Location) (domain.LLMUsageSummary, error) {
	return domain.LLMUsageSummary{}, nil
}

func (f *fakeUsageStore) snapshot() []domain.LLMUsageRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.LLMUsageRecord, len(f.records))
	copy(out, f.records)
	return out
}

// AGY's stream never reports a model on the session (unlike claudecode's
// init event), so the meter must fall back to the model the task asked for.
func TestExecuteRecordsCLIUsage(t *testing.T) {
	store := &fakeUsageStore{}
	ex, workDir := newTestExecutor(t, Config{Usage: usageapp.NewMeter(store)}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	got := store.snapshot()[0]
	assert.Equal(t, domain.LLMUsageKindCLI, got.Kind)
	assert.Equal(t, string(domain.LLMProviderAntigravity), got.Provider)
	assert.Equal(t, "gemini-3.1-pro-high", got.Model, "no model comes back on the stream, so the requested model is what gets recorded")
	assert.Equal(t, 1500, got.PromptTokens)
	assert.Equal(t, 800, got.CompletionTokens)
	assert.Equal(t, 200, got.CacheReadTokens)
}

// A session that ends in a quota park still burned tokens (the fixture's
// result event reports usage even though the status isn't SUCCESS), so the
// meter must still see it.
func TestExecuteRecordsCLIUsageOnQuotaBlock(t *testing.T) {
	store := &fakeUsageStore{}
	ex, workDir := newTestExecutor(t, Config{Usage: usageapp.NewMeter(store)}, "quota_with_usage.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	var block *domain.QuotaBlock
	require.True(t, errors.As(err, &block))

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	got := store.snapshot()[0]
	assert.Equal(t, domain.LLMUsageKindCLI, got.Kind)
	assert.Equal(t, 900, got.PromptTokens)
	assert.Equal(t, 40, got.CompletionTokens)
}

// A nil Meter (Config{} with no Usage set, as every other executor test in
// this package does) must not panic the finish path.
func TestExecuteWithNoMeterConfiguredDoesNotPanic(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	require.NotPanics(t, func() {
		_, err := ex.Execute(context.Background(), taskExecution(workDir))
		require.NoError(t, err)
	})
}
