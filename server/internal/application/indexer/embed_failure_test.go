package indexer_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func newEmbedTestService(store *fakeIndexStore, llm *fakeLLM, cfg domain.IndexerConfig) *indexer.Service {
	cfg.Enabled = true
	cfg.TopK = 5
	return indexer.NewService(
		store,
		llm,
		mapper.NewService(domain.MappingConfig{Enabled: true, MaxFiles: 50, TreeMaxDepth: 4}),
		chunker.DefaultRegistry(),
		cfg,
		domain.GraphConfig{Enabled: true},
		"embed-model",
	)
}

// A connection the embedder dropped under a request is retried for the same
// chunk instead of failing the index.
func TestIndexRetriesATransientEmbedderError(t *testing.T) {
	var calls atomic.Int64
	llm := &fakeLLM{embedFn: func(_ context.Context, input string, _ string) ([]float32, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New(`embeddings unreachable: Post "http://127.0.0.1:1/embeddings": read: connection reset by peer`)
		}
		return []float32{float32(len(input)), 1}, nil
	}}
	svc := newEmbedTestService(newFakeIndexStore(), llm, domain.IndexerConfig{})

	idx, err := svc.IndexSession(context.Background(), uuid.New(), mapperFixtureRoot())
	require.NoError(t, err)
	require.Equal(t, domain.IndexStatusCompleted, idx.Status)
	require.Greater(t, calls.Load(), int64(1))
}

// A chunk the embedder cannot take is stored without a vector and the rest of
// the repository is still indexed.
func TestIndexCompletesWhenSomeChunksCannotBeEmbedded(t *testing.T) {
	var calls, failures atomic.Int64
	llm := &fakeLLM{embedFn: func(_ context.Context, input string, _ string) ([]float32, error) {
		if calls.Add(1)%4 == 0 {
			failures.Add(1)
			return nil, errors.New("embeddings returned 500: inference_failed")
		}
		return []float32{float32(len(input)), 1}, nil
	}}
	svc := newEmbedTestService(newFakeIndexStore(), llm, domain.IndexerConfig{Concurrency: 1})

	idx, err := svc.IndexSession(context.Background(), uuid.New(), mapperFixtureRoot())
	require.NoError(t, err)
	require.Equal(t, domain.IndexStatusCompleted, idx.Status)
	require.Greater(t, failures.Load(), int64(0), "the fixture must exercise at least one failed chunk")
	require.Greater(t, idx.ChunkCount, 0)
}

// An embedder that refuses everything stops the pass instead of storing a
// whole repository of chunks nothing can find by meaning.
func TestIndexFailsAfterManyConsecutiveEmbedFailures(t *testing.T) {
	llm := &fakeLLM{embedFn: func(context.Context, string, string) ([]float32, error) {
		return nil, errors.New("embeddings returned 500: inference_failed")
	}}
	store := newFakeIndexStore()
	svc := newEmbedTestService(store, llm, domain.IndexerConfig{})
	sessionID := uuid.New()

	_, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot())
	require.Error(t, err)
	require.Contains(t, err.Error(), "could not be embedded")

	stored, getErr := store.GetIndexBySession(context.Background(), sessionID)
	require.NoError(t, getErr)
	require.Equal(t, domain.IndexStatusFailed, stored.Status)
}

// However many files are read in parallel, embedding calls stay within the
// shared limit.
func TestIndexKeepsEmbedCallsWithinTheSharedLimit(t *testing.T) {
	var inFlight, peak atomic.Int64
	llm := &fakeLLM{embedFn: func(_ context.Context, input string, _ string) ([]float32, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(3 * time.Millisecond)
		inFlight.Add(-1)
		return []float32{float32(len(input)), 1}, nil
	}}
	svc := newEmbedTestService(newFakeIndexStore(), llm, domain.IndexerConfig{Concurrency: 8, EmbedConcurrency: 2})

	idx, err := svc.IndexSession(context.Background(), uuid.New(), mapperFixtureRoot())
	require.NoError(t, err)
	require.Equal(t, domain.IndexStatusCompleted, idx.Status)
	require.LessOrEqual(t, peak.Load(), int64(2))
	require.Greater(t, peak.Load(), int64(0))
}

// A file with a chunk that could not be embedded keeps no hash, so the next
// incremental pass embeds that file again, and only the files that need it.
func TestNextPassReembedsOnlyFilesWithUnembeddedChunks(t *testing.T) {
	var failNow atomic.Bool
	failNow.Store(true)
	var firstPass, secondPass atomic.Int64
	llm := &fakeLLM{embedFn: func(_ context.Context, input string, _ string) ([]float32, error) {
		if failNow.Load() {
			if firstPass.Add(1)%4 == 0 {
				return nil, errors.New("embeddings returned 500: inference_failed")
			}
			return []float32{float32(len(input)), 1}, nil
		}
		secondPass.Add(1)
		return []float32{float32(len(input)), 1}, nil
	}}
	store := newFakeIndexStore()
	svc := newEmbedTestService(store, llm, domain.IndexerConfig{Concurrency: 1, ReindexOnChange: true})
	sessionID := uuid.New()

	_, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot())
	require.NoError(t, err)

	failNow.Store(false)
	resumed, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot())
	require.NoError(t, err)
	require.Equal(t, domain.IndexStatusCompleted, resumed.Status)
	require.Greater(t, secondPass.Load(), int64(0), "files with an unembedded chunk must be embedded again")
	require.Less(t, secondPass.Load(), firstPass.Load(), "files that were fully embedded must not be embedded again")
}
