package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type Service struct {
	store          port.AgentMemoryStore
	llm            port.LLMClient
	embeddingModel string
	maxCount       int
}

func NewService(store port.AgentMemoryStore, llm port.LLMClient, embeddingModel string, maxCount int) *Service {
	if maxCount <= 0 {
		maxCount = 200
	}
	return &Service{store: store, llm: llm, embeddingModel: embeddingModel, maxCount: maxCount}
}

// Save stores a memory in one of the four buckets: agentID uuid.Nil makes it a
// team memory, a nil repositoryID makes it global instead of project-scoped.
// Embedding failures are non-fatal: the memory is stored without an embedding
// and remains recallable by recency.
func (s *Service) Save(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, content, category, source string) (domain.AgentMemory, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.AgentMemory{}, fmt.Errorf("content is required")
	}
	if source == "" {
		source = domain.MemorySourceAgent
	}
	var embedding []float32
	if s.llm != nil {
		emb, err := s.llm.Embed(ctx, content, s.embeddingModel)
		if err != nil {
			log.Warn().Err(err).Msg("memory embedding failed, storing without embedding")
		} else {
			embedding = emb
		}
	}
	mem, err := s.store.Create(ctx, domain.AgentMemory{
		AgentID: agentID, RepositoryID: repositoryID, Content: content,
		Category: strings.TrimSpace(category), Embedding: embedding, Source: source,
	})
	if err != nil {
		return domain.AgentMemory{}, err
	}
	// Team memories (agentID == uuid.Nil) are exempt from eviction, and each
	// project bucket is capped on its own so a busy repository cannot push out
	// what the agent learned elsewhere.
	if agentID != uuid.Nil {
		if n, cErr := s.store.CountInScope(ctx, agentID, repositoryID); cErr == nil && n > s.maxCount {
			if dErr := s.store.DeleteOldestInScope(ctx, agentID, repositoryID, n-s.maxCount); dErr != nil {
				log.Warn().Err(dErr).Msg("memory eviction failed")
			}
		}
	}
	return mem, nil
}

// SaveShared stores a team memory every agent can read. A repositoryID keeps it
// to that project; nil makes it workspace-wide.
func (s *Service) SaveShared(ctx context.Context, repositoryID *uuid.UUID, content, category, source string) (domain.AgentMemory, error) {
	return s.Save(ctx, uuid.Nil, repositoryID, content, category, source)
}

// Search returns semantically ranked memories from the buckets the query
// selects; an empty query or embedding failure falls back to recency.
func (s *Service) Search(ctx context.Context, q domain.MemoryQuery, query string, topK int) ([]domain.AgentMemory, error) {
	if topK <= 0 {
		topK = 5
	}
	query = strings.TrimSpace(query)
	if query == "" || s.llm == nil {
		q.Limit = topK
		return s.store.List(ctx, q)
	}
	emb, err := s.llm.Embed(ctx, query, s.embeddingModel)
	if err != nil {
		log.Warn().Err(err).Msg("memory query embedding failed, falling back to recency")
		q.Limit = topK
		return s.store.List(ctx, q)
	}
	q.Limit = 1000
	candidates, err := s.store.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return rankByEmbedding(candidates, emb, topK), nil
}

func (s *Service) List(ctx context.Context, q domain.MemoryQuery) ([]domain.AgentMemory, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	return s.store.List(ctx, q)
}

// Recall reads what an agent should carry into a run: the repository's project
// memories and the global ones, each bucket bounded on its own so a chatty
// project cannot crowd out durable workspace knowledge.
func (s *Service) Recall(ctx context.Context, agentID uuid.UUID, repositoryID *uuid.UUID, perScope int) []domain.AgentMemory {
	return Recall(ctx, s.store, agentID, repositoryID, perScope)
}

// Recall is the store-level form used by callers that hold the port rather than
// the service (the board runner and the chat session loop).
func Recall(ctx context.Context, store port.AgentMemoryStore, agentID uuid.UUID, repositoryID *uuid.UUID, perScope int) []domain.AgentMemory {
	if store == nil {
		return nil
	}
	if perScope <= 0 {
		perScope = 8
	}
	var out []domain.AgentMemory
	if repositoryID != nil {
		project, err := store.List(ctx, domain.MemoryQuery{
			AgentID: agentID, RepositoryID: repositoryID,
			Repo: domain.MemoryRepoScopeProject, Limit: perScope,
		})
		if err != nil {
			log.Warn().Err(err).Msg("project memory recall failed")
		} else {
			out = append(out, project...)
		}
	}
	global, err := store.List(ctx, domain.MemoryQuery{
		AgentID: agentID, Repo: domain.MemoryRepoScopeGlobal, Limit: perScope,
	})
	if err != nil {
		log.Warn().Err(err).Msg("global memory recall failed")
		return out
	}
	return append(out, global...)
}

func (s *Service) Update(ctx context.Context, agentID, memoryID uuid.UUID, content, category string) (domain.AgentMemory, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.AgentMemory{}, fmt.Errorf("content is required")
	}
	mem, err := s.store.Get(ctx, memoryID)
	if err != nil {
		return domain.AgentMemory{}, err
	}
	if mem.AgentID != uuid.Nil && mem.AgentID != agentID {
		return domain.AgentMemory{}, fmt.Errorf("memory does not belong to this agent")
	}
	if mem.AgentID == uuid.Nil && agentID != uuid.Nil {
		return domain.AgentMemory{}, fmt.Errorf("use shared memory endpoint for team memories")
	}
	var embedding []float32
	if s.llm != nil {
		emb, err := s.llm.Embed(ctx, content, s.embeddingModel)
		if err != nil {
			log.Warn().Err(err).Msg("memory embedding failed on update, storing without embedding")
		} else {
			embedding = emb
		}
	}
	return s.store.Update(ctx, domain.AgentMemory{
		ID: mem.ID, AgentID: mem.AgentID, RepositoryID: mem.RepositoryID, Content: content,
		Category: strings.TrimSpace(category), Embedding: embedding, Source: mem.Source,
	})
}

func (s *Service) UpdateShared(ctx context.Context, memoryID uuid.UUID, content, category string) (domain.AgentMemory, error) {
	return s.Update(ctx, uuid.Nil, memoryID, content, category)
}

// Delete removes a memory after verifying it belongs to the agent.
// Team memories (AgentID == uuid.Nil) may be deleted by any agent.
func (s *Service) Delete(ctx context.Context, agentID, memoryID uuid.UUID) error {
	mem, err := s.store.Get(ctx, memoryID)
	if err != nil {
		return err
	}
	if mem.AgentID != uuid.Nil && mem.AgentID != agentID {
		return fmt.Errorf("memory does not belong to this agent")
	}
	return s.store.Delete(ctx, memoryID)
}

func rankByEmbedding(memories []domain.AgentMemory, embedding []float32, topK int) []domain.AgentMemory {
	type scored struct {
		mem   domain.AgentMemory
		score float64
	}
	results := make([]scored, 0, len(memories))
	for _, m := range memories {
		results = append(results, scored{mem: m, score: cosineSimilarity(embedding, m.Embedding)})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	if topK > len(results) {
		topK = len(results)
	}
	out := make([]domain.AgentMemory, topK)
	for i := 0; i < topK; i++ {
		out[i] = results[i].mem
	}
	return out
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
