package indexer

import (
	"context"
	"sync/atomic"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// passProvenance is what ONE index pass will record about itself: the embedding
// model it is running on, and the vector length that model actually returned.
//
// It exists because "what this index was built with" is knowable in exactly one
// place — inside the pass, while it is making the embed calls — and nowhere
// else afterwards. Deriving it later from configuration is precisely the
// mistake migration 116 was written to end: configuration is the thing that
// MOVES, and an index built last week says nothing about what the tenant
// switched to yesterday.
//
// The dimension is observed rather than assumed. llmprovider.Service
// .ResolvedEmbedding answers 0 for any model whose size it does not know (it
// only knows the one pinned local model), and a pass holds something far
// better: a vector it just received. A recorded 0 means "this pass embedded
// nothing measurable", which domain.EmbeddingProvenanceStale treats as unknown
// rather than as a mismatch — the honest answer, and the one that will not
// invalidate an index over a fact nobody established.
type passProvenance struct {
	// model is the embedding model this pass is running on, resolved once at
	// the start. Blank when nothing can answer (no resolver wired and no model
	// configured), which switches the whole guard off rather than making
	// everything look stale.
	model string
	// configuredDims is the resolver's own answer, used only when the pass
	// observes no vector of its own.
	configuredDims int
	// observedDims is the length of the first embedding this pass received.
	// Written from several worker goroutines, so it is atomic and first-writer-
	// wins: every vector in one pass comes from one model, so the first is as
	// good as any, and CompareAndSwap avoids the lock a mutex would put on the
	// hot path of every chunk.
	observedDims atomic.Int64
}

// newPassProvenance resolves what this pass will embed with, through the same
// resolver the search guard and the status warning consult
// (fallbackEmbeddingResolver), so all three agree on what "the configured
// model" means.
//
// A resolver error is not fatal and does not fall back to a guess: the pass
// keeps the model this service was constructed with. Refusing to index because
// a settings lookup failed would trade a hypothetical mismatch for a certain
// outage, and recording a guess would be worse than recording the truth of what
// was asked for.
func (s *Service) newPassProvenance(ctx context.Context) *passProvenance {
	p := &passProvenance{model: s.embeddingModel}
	if s.embeddings == nil {
		return p
	}
	model, dims, err := s.embeddings.ResolvedEmbedding(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("could not resolve the configured embedding model; this pass records the model it was configured with")
		return p
	}
	p.model = model
	p.configuredDims = dims
	return p
}

// observe records the length of a vector the pass received. Called for every
// chunk; only the first one changes anything.
func (p *passProvenance) observe(embedding []float32) {
	if p == nil || len(embedding) == 0 {
		return
	}
	p.observedDims.CompareAndSwap(0, int64(len(embedding)))
}

// modelName is what gets stamped as the model. Blank is a legitimate value —
// it says this deployment could not name what it embedded with — and reads back
// as "unknown", which EmbeddingProvenanceStale refuses to treat as a match.
func (p *passProvenance) modelName() string {
	if p == nil {
		return ""
	}
	return p.model
}

// dimensions is what gets stamped: the measured length if this pass embedded
// anything, otherwise whatever the resolver claimed, otherwise 0 for unknown.
func (p *passProvenance) dimensions() int {
	if p == nil {
		return 0
	}
	if observed := p.observedDims.Load(); observed > 0 {
		return int(observed)
	}
	return p.configuredDims
}

// staleAgainst reports that an existing index's vectors were not produced by
// what this pass is running on, so an incremental pass over it would mix two
// models' coordinate systems into one index.
//
// It compares against the resolver's dimension, not the observed one: nothing
// has been embedded yet when this is asked.
//
// The blank case is deliberately narrower here than at read time, and this is
// the one place the two differ. A blank model means one of two things, and they
// want opposite treatment:
//
//	an index that PREDATES migration 116 — its rows came from some model
//	  nobody recorded, quite possibly not this one, so it has to be rebuilt.
//	an index whose last pass was INTERRUPTED — the provenance is stamped at
//	  the end, so a pass that was stopped, crashed or timed out leaves the
//	  column blank while its per-file rows and hashes are perfectly good.
//
// Only the first is distinguishable, and only by status: a pass that finishes
// always stamps, so a COMPLETED index with no model is one from before the
// column existed. Anything else is a pass that did not finish, and forcing a
// full re-embed on it would throw away every file the interrupted run had
// already done — reinstating exactly the "every interrupted run starts from
// zero" behaviour that per-file persistence exists to prevent.
//
// Read time makes the opposite call (domain.EmbeddingProvenanceStale treats any
// blank as stale) because the cost is reversed there: refusing to search an
// index whose provenance is unknown costs one re-index, while searching it
// costs a confidently wrong answer.
func (p *passProvenance) staleAgainst(index domain.WorkspaceIndex) bool {
	if p == nil {
		return false
	}
	if index.EmbeddingModel == "" && index.Status != domain.IndexStatusCompleted {
		return false
	}
	return domain.EmbeddingProvenanceStale(index.EmbeddingModel, index.EmbeddingDims, p.model, p.configuredDims)
}

// AnnotateEmbeddingProvenance fills in the read-time half of the guard on an
// index a caller is about to show a person: is what it holds still comparable
// with what this tenant embeds queries with, and if not, what to say about it.
//
// It lives on the indexer because the indexer is what holds the resolver, and
// because the alternative — every consumer of an index status resolving the
// tenant's embedding setting for itself — is how two pages end up disagreeing
// about whether the same index is usable.
//
// Nothing is annotated when no model can be resolved: with nothing to compare
// against, "stale" is not a verdict this can reach, and defaulting to it would
// mark every index on a self-hosted deployment as broken.
func (s *Service) AnnotateEmbeddingProvenance(ctx context.Context, index *domain.WorkspaceIndex) {
	if s == nil || index == nil {
		return
	}
	if s.embeddings == nil {
		return
	}
	model, dims, err := s.embeddings.ResolvedEmbedding(ctx)
	if err != nil {
		log.Warn().Err(err).Str("index_id", index.ID.String()).
			Msg("could not resolve the configured embedding model; index staleness not reported")
		return
	}
	if !domain.EmbeddingProvenanceStale(index.EmbeddingModel, index.EmbeddingDims, model, dims) {
		return
	}
	index.EmbeddingStale = true
	index.EmbeddingWarning = domain.EmbeddingStaleMessage(index.EmbeddingModel, index.EmbeddingDims, model, dims)
}
