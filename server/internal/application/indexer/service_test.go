package indexer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type ServiceSuite struct {
	suite.Suite
	store     *fakeIndexStore
	llm       *fakeLLM
	svc       *indexer.Service
	sessionID uuid.UUID
}

func (s *ServiceSuite) SetupTest() {
	s.store = newFakeIndexStore()
	s.llm = &fakeLLM{}
	s.sessionID = uuid.New()
	s.svc = indexer.NewService(
		s.store,
		s.llm,
		mapper.NewService(domain.MappingConfig{
			Enabled:      true,
			MaxFiles:     50,
			TreeMaxDepth: 4,
		}),
		chunker.DefaultRegistry(),
		domain.IndexerConfig{
			Enabled:         true,
			TopK:            5,
			ReindexOnChange: true,
		},
		domain.GraphConfig{Enabled: true},
		"embed-model",
	)
}

func (s *ServiceSuite) TestIndexSessionPipeline() {
	idx, err := s.svc.IndexSession(context.Background(), s.sessionID, mapperFixtureRoot())
	s.Require().NoError(err)
	s.Equal(domain.IndexStatusCompleted, idx.Status)
	s.Greater(idx.FileCount, 0)
	s.Greater(idx.ChunkCount, 0)
	s.Greater(idx.SymbolCount, 0)
	s.NotEmpty(idx.TreeText)

	stored, err := s.store.GetIndexBySession(context.Background(), s.sessionID)
	s.Require().NoError(err)
	s.Equal(domain.IndexStatusCompleted, stored.Status)

	hashes, err := s.store.GetFileHashes(context.Background(), idx.ID)
	s.Require().NoError(err)
	s.NotEmpty(hashes)
}

func (s *ServiceSuite) TestIndexSessionIncrementalSkipsUnchanged() {
	first, err := s.svc.IndexSession(context.Background(), s.sessionID, mapperFixtureRoot())
	s.Require().NoError(err)

	second, err := s.svc.IndexSession(context.Background(), s.sessionID, mapperFixtureRoot())
	s.Require().NoError(err)
	s.Equal(domain.IndexStatusCompleted, second.Status)
	s.Equal(first.ChunkCount, second.ChunkCount)
	s.Equal(first.SymbolCount, second.SymbolCount)
}

func (s *ServiceSuite) TestInterruptedPassKeepsFinishedFilesAndResumes() {

	passCtx, cancelPass := context.WithCancel(context.Background())
	defer cancelPass()
	var firstFile string
	s.llm.embedFn = func(ctx context.Context, input string, _ string) ([]float32, error) {
		path := strings.Fields(input)[0]
		if firstFile == "" {
			firstFile = path
		}
		if path != firstFile {
			cancelPass()
			return nil, ctx.Err()
		}
		return []float32{float32(len(input))}, nil
	}

	_, err := s.svc.IndexSession(passCtx, s.sessionID, mapperFixtureRoot())
	s.Require().Error(err, "a cancelled pass must fail, not report a finished index")

	idx, err := s.store.GetIndexBySession(context.Background(), s.sessionID)
	s.Require().NoError(err)
	s.Equal(domain.IndexStatusFailed, idx.Status)

	done, err := s.store.GetFileHashes(context.Background(), idx.ID)
	s.Require().NoError(err)
	s.NotEmpty(done, "the file that finished before the failure kept its rows and its hash")

	var reEmbedded []string
	s.llm.embedFn = func(_ context.Context, input string, _ string) ([]float32, error) {
		reEmbedded = append(reEmbedded, strings.Fields(input)[0])
		return []float32{float32(len(input))}, nil
	}
	resumed, err := s.svc.IndexSession(context.Background(), s.sessionID, mapperFixtureRoot())
	s.Require().NoError(err)
	s.Equal(domain.IndexStatusCompleted, resumed.Status)

	full, err := s.store.GetFileHashes(context.Background(), resumed.ID)
	s.Require().NoError(err)
	s.Greater(len(full), len(done), "the resume indexed the files the failed pass never reached")
	for _, path := range reEmbedded {
		s.NotContains(done, path, "a file the first pass finished was embedded again")
	}
}

func (s *ServiceSuite) TestRateLimitedEmbeddingIsWaitedOutNotFailed() {
	var calls int
	s.llm.embedFn = func(_ context.Context, input string, _ string) ([]float32, error) {
		calls++
		if calls == 1 {

			return nil, errors.New("embeddings returned 429: too many requests")
		}
		return []float32{float32(len(input))}, nil
	}

	idx, err := s.svc.IndexSession(context.Background(), s.sessionID, mapperFixtureRoot())
	s.Require().NoError(err)
	s.Equal(domain.IndexStatusCompleted, idx.Status)
	s.Greater(idx.ChunkCount, 0, "the rate-limited chunk was embedded on the retry, not skipped")
}

func (s *ServiceSuite) TestGetStatus() {
	_, err := s.svc.IndexSession(context.Background(), s.sessionID, mapperFixtureRoot())
	s.Require().NoError(err)

	status, err := s.svc.GetStatus(context.Background(), s.sessionID)
	s.Require().NoError(err)
	s.Equal(domain.IndexStatusCompleted, status.Status)
}

func (s *ServiceSuite) TestIndexSessionDisabled() {
	disabled := indexer.NewService(
		s.store,
		s.llm,
		mapper.NewService(domain.MappingConfig{Enabled: true}),
		chunker.DefaultRegistry(),
		domain.IndexerConfig{Enabled: false},
		domain.GraphConfig{},
		"",
	)
	_, err := disabled.IndexSession(context.Background(), s.sessionID, mapperFixtureRoot())
	s.Error(err)
}

func (s *ServiceSuite) TestSearchProject() {
	projectID := uuid.New()
	idx, err := s.store.CreateProjectIndex(context.Background(), projectID, mapperFixtureRoot(), "tree")
	s.Require().NoError(err)
	idx.Status = domain.IndexStatusCompleted
	s.store.UpdateIndexStatus(context.Background(), idx.ID, domain.IndexStatusCompleted, 1, 1, 1, "")

	chunkID := uuid.New()
	s.store.SaveChunks(context.Background(), idx.ID, []domain.WorkspaceChunk{{
		ID:         chunkID,
		IndexID:    idx.ID,
		FilePath:   "auth/middleware.go",
		SymbolName: "AuthMiddleware",
		Kind:       "function",
		StartLine:  10,
		EndLine:    40,
		Content:    "func AuthMiddleware() {}",
		Embedding:  []float32{1, 0, 0},
	}})

	results, err := s.svc.SearchProject(context.Background(), projectID, "authentication middleware", 5)
	s.Require().NoError(err)
	s.NotEmpty(results)
	s.Equal("auth/middleware.go", results[0].FilePath)
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}
