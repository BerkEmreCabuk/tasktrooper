package claudecode

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

// A finished session must meter its usage as one kind=cli row, with the
// model the CLI's own init event reported (not the model the task asked
// for — the CLI can pick a different one) and the provider fixed to
// claude_code.
func TestExecuteRecordsCLIUsage(t *testing.T) {
	store := &fakeUsageStore{}
	ex, workDir := newTestExecutor(t, Config{Usage: usageapp.NewMeter(store)}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	got := store.snapshot()[0]
	assert.Equal(t, domain.LLMUsageKindCLI, got.Kind)
	assert.Equal(t, string(domain.LLMProviderClaudeCode), got.Provider)
	assert.Equal(t, "claude-opus-5", got.Model, "the init event's own model wins over the invocation's requested model")
	assert.Equal(t, 13700, got.PromptTokens)
	assert.Equal(t, 800, got.CompletionTokens)
}

// A session that ends in a quota block still burned tokens (the fixture's
// result event reports usage even though it is an error), so the meter must
// still see it: a park must not look free.
func TestExecuteRecordsCLIUsageOnQuotaBlock(t *testing.T) {
	store := &fakeUsageStore{}
	ex, workDir := newTestExecutor(t, Config{Usage: usageapp.NewMeter(store)}, "usage_limit.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	var block *domain.QuotaBlock
	require.True(t, errors.As(err, &block))

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	got := store.snapshot()[0]
	assert.Equal(t, domain.LLMUsageKindCLI, got.Kind)
	assert.Equal(t, "claude-opus-5", got.Model)
	assert.Equal(t, 300, got.PromptTokens)
	assert.Equal(t, 50, got.CompletionTokens)
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
