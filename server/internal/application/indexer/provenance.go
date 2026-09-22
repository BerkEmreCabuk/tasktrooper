package indexer

import (
	"context"
	"sync/atomic"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

type passProvenance struct {
	model string

	configuredDims int

	observedDims atomic.Int64

	embedFailStreak atomic.Int64
}

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

func (p *passProvenance) observe(embedding []float32) {
	if p == nil || len(embedding) == 0 {
		return
	}
	p.observedDims.CompareAndSwap(0, int64(len(embedding)))
}

func (p *passProvenance) modelName() string {
	if p == nil {
		return ""
	}
	return p.model
}

func (p *passProvenance) dimensions() int {
	if p == nil {
		return 0
	}
	if observed := p.observedDims.Load(); observed > 0 {
		return int(observed)
	}
	return p.configuredDims
}

func (p *passProvenance) staleAgainst(index domain.WorkspaceIndex) bool {
	if p == nil {
		return false
	}
	if index.EmbeddingModel == "" && index.Status != domain.IndexStatusCompleted {
		return false
	}
	return domain.EmbeddingProvenanceStale(index.EmbeddingModel, index.EmbeddingDims, p.model, p.configuredDims)
}

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

func (p *passProvenance) embedFailed() int64 {
	if p == nil {
		return 0
	}
	return p.embedFailStreak.Add(1)
}

func (p *passProvenance) embedSucceeded() {
	if p != nil {
		p.embedFailStreak.Store(0)
	}
}
