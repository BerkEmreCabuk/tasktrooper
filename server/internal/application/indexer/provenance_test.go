package indexer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeEmbeddingResolver stands in for llmprovider.Service: what the tenant's
// embed calls resolve to right now, which is the half of the staleness
// comparison that lives outside the index row.
type fakeEmbeddingResolver struct {
	model string
	dims  int
	err   error
}

func (f *fakeEmbeddingResolver) ResolvedEmbedding(context.Context) (string, int, error) {
	return f.model, f.dims, f.err
}

func newProvenanceService(t *testing.T, store *fakeIndexStore, llm *fakeLLM) *indexer.Service {
	return newProvenanceServiceWithModel(t, store, llm, "configured-embed-model")
}

func newProvenanceServiceWithModel(t *testing.T, store *fakeIndexStore, llm *fakeLLM, model string) *indexer.Service {
	t.Helper()
	return indexer.NewService(
		store,
		llm,
		mapper.NewService(domain.MappingConfig{Enabled: true, MaxFiles: 50, TreeMaxDepth: 4}),
		chunker.DefaultRegistry(),
		domain.IndexerConfig{Enabled: true, TopK: 5, ReindexOnChange: true},
		domain.GraphConfig{Enabled: true},
		model,
	)
}

// A finished pass must record what it embedded WITH, not just what it embedded.
// Without this the index is indistinguishable from one built by any other
// model, and every later staleness check has nothing to compare against.
//
// The dimension is asserted against the vector the pass actually received
// rather than against the resolver's number, because that is the point of
// observing it: the resolver answers 0 for a model whose size it does not know,
// and a pass always holds something better.
func TestIndexPassRecordsWhatItEmbeddedWith(t *testing.T) {
	store := newFakeIndexStore()
	llm := &fakeLLM{embedFn: func(context.Context, string, string) ([]float32, error) {
		return make([]float32, 384), nil
	}}
	svc := newProvenanceService(t, store, llm)
	// dims 0 on purpose: this resolver knows the model's name and not its size,
	// exactly like llmprovider.Service for anything but the pinned local model.
	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: "nomic-embed-text-v1.5"})

	sessionID := uuid.New()
	idx, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot())
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if idx.EmbeddingModel != "nomic-embed-text-v1.5" {
		t.Fatalf("returned index recorded model %q", idx.EmbeddingModel)
	}
	if idx.EmbeddingDims != 384 {
		t.Fatalf("returned index recorded %d dimensions, want the 384 the model actually returned", idx.EmbeddingDims)
	}

	stored, err := store.GetIndexBySession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.EmbeddingModel != "nomic-embed-text-v1.5" || stored.EmbeddingDims != 384 {
		t.Fatalf("stored provenance = %q/%d, want nomic-embed-text-v1.5/384", stored.EmbeddingModel, stored.EmbeddingDims)
	}
}

// With no resolver the pass still records something honest: the model this
// service was configured with. A self-hosted deployment has no per-tenant
// setting to move underneath it, so that name is the whole truth there — and
// recording nothing would make every such index read as stale the moment a
// resolver was ever wired.
func TestIndexPassFallsBackToTheConfiguredModel(t *testing.T) {
	store := newFakeIndexStore()
	svc := newProvenanceService(t, store, &fakeLLM{})

	idx, err := svc.IndexSession(context.Background(), uuid.New(), mapperFixtureRoot())
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if idx.EmbeddingModel != "configured-embed-model" {
		t.Fatalf("recorded model %q, want the configured one", idx.EmbeddingModel)
	}
}

// A resolver that cannot answer must not stop an index from being built, and
// must not cause a guess to be recorded either. Trading an index for a failed
// settings lookup is the wrong direction.
func TestIndexPassSurvivesAnUnansweredResolver(t *testing.T) {
	store := newFakeIndexStore()
	svc := newProvenanceService(t, store, &fakeLLM{})
	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{err: errors.New("settings unavailable")})

	idx, err := svc.IndexSession(context.Background(), uuid.New(), mapperFixtureRoot())
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if idx.Status != domain.IndexStatusCompleted {
		t.Fatalf("status = %s, want completed", idx.Status)
	}
	if idx.EmbeddingModel != "configured-embed-model" {
		t.Fatalf("recorded model %q, want the configured one", idx.EmbeddingModel)
	}
}

// AnnotateEmbeddingProvenance is what the repository index-status endpoint
// calls, so it is what decides whether a person sees "completed 100%" or
// "completed 100%, and unusable until you re-index".
func TestAnnotateEmbeddingProvenance(t *testing.T) {
	cases := []struct {
		name       string
		index      domain.WorkspaceIndex
		resolver   *fakeEmbeddingResolver
		configured string
		wantStale  bool
		wantPhrase string
	}{
		{
			name:      "same model is not stale",
			index:     domain.WorkspaceIndex{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDims: 768},
			resolver:  &fakeEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768},
			wantStale: false,
		},
		{
			name:       "a different model is stale and says which",
			index:      domain.WorkspaceIndex{EmbeddingModel: "text-embedding-3-small", EmbeddingDims: 1536},
			resolver:   &fakeEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768},
			wantStale:  true,
			wantPhrase: "text-embedding-3-small",
		},
		{
			// The pre-116 case: the index predates the columns, so it records
			// no model at all. "Unknown" is not "matches".
			name:       "an index with no recorded model is stale once a model is configured",
			index:      domain.WorkspaceIndex{},
			resolver:   &fakeEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768},
			wantStale:  true,
			wantPhrase: "predates embedding provenance tracking",
		},
		{
			// Same pre-116 index, but nothing anywhere names a model to compare
			// it against — no resolver answer and no configured model. Marking
			// it stale here would condemn every index on a deployment that has
			// not chosen an embedding provider.
			name:       "an index with no recorded model is not stale when nothing is configured",
			index:      domain.WorkspaceIndex{},
			resolver:   &fakeEmbeddingResolver{},
			configured: "-",
			wantStale:  false,
		},
		{
			// Same name, different output size: a provider that reuses a model
			// name across versions would otherwise pass on the name alone.
			name:       "the same name at a different size is stale",
			index:      domain.WorkspaceIndex{EmbeddingModel: "embed-v1", EmbeddingDims: 768},
			resolver:   &fakeEmbeddingResolver{model: "embed-v1", dims: 1536},
			wantStale:  true,
			wantPhrase: "1536",
		},
		{
			// A lookup failure is not a verdict.
			name:      "an unanswered resolver reports nothing",
			index:     domain.WorkspaceIndex{EmbeddingModel: "text-embedding-3-small", EmbeddingDims: 1536},
			resolver:  &fakeEmbeddingResolver{err: errors.New("settings unavailable")},
			wantStale: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// "-" means this deployment names no model at all, anywhere.
			configured := "configured-embed-model"
			if tc.configured == "-" {
				configured = ""
			}
			svc := newProvenanceServiceWithModel(t, newFakeIndexStore(), &fakeLLM{}, configured)
			svc.SetEmbeddingResolver(tc.resolver)

			idx := tc.index
			svc.AnnotateEmbeddingProvenance(context.Background(), &idx)

			if idx.EmbeddingStale != tc.wantStale {
				t.Fatalf("EmbeddingStale = %v, want %v (warning %q)", idx.EmbeddingStale, tc.wantStale, idx.EmbeddingWarning)
			}
			if !tc.wantStale {
				if idx.EmbeddingWarning != "" {
					t.Fatalf("a usable index carried a warning: %q", idx.EmbeddingWarning)
				}
				return
			}
			if idx.EmbeddingWarning == "" {
				t.Fatal("a stale index carried no sentence explaining why")
			}
			if tc.wantPhrase != "" && !contains(idx.EmbeddingWarning, tc.wantPhrase) {
				t.Fatalf("warning %q does not mention %q", idx.EmbeddingWarning, tc.wantPhrase)
			}
			if !contains(idx.EmbeddingWarning, "Re-index") {
				t.Fatalf("warning %q does not say what to do about it", idx.EmbeddingWarning)
			}
		})
	}
}

// A pass over an index built by a different model must re-embed every file, not
// only the changed ones: an incremental pass would leave the unchanged files'
// old vectors next to the new ones, and one index would hold two incomparable
// coordinate systems while claiming, in its own provenance column, to hold one.
func TestStaleIndexIsReEmbeddedWhole(t *testing.T) {
	store := newFakeIndexStore()
	embeds := 0
	llm := &fakeLLM{embedFn: func(context.Context, string, string) ([]float32, error) {
		embeds++
		return make([]float32, 768), nil
	}}
	svc := newProvenanceService(t, store, llm)
	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: "model-a", dims: 768})

	sessionID := uuid.New()
	if _, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	first := embeds
	if first == 0 {
		t.Fatal("first pass embedded nothing")
	}

	// Nothing on disk changed, so an ordinary second pass embeds nothing.
	embeds = 0
	if _, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if embeds != 0 {
		t.Fatalf("an unchanged tree re-embedded %d chunks", embeds)
	}

	// Now the tenant switches model. The tree is still unchanged — and every
	// chunk must be embedded again anyway.
	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: "model-b", dims: 1536})
	embeds = 0
	idx, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot())
	if err != nil {
		t.Fatalf("third pass: %v", err)
	}
	if embeds != first {
		t.Fatalf("a model change re-embedded %d chunks, want all %d", embeds, first)
	}
	if idx.EmbeddingModel != "model-b" {
		t.Fatalf("index still records %q after re-embedding with model-b", idx.EmbeddingModel)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
