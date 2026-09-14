package rag

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	store          port.FileStore
	llm            port.LLMClient
	storageDir     string
	chunkSize      int
	chunkOverlap   int
	topK           int
	embeddingModel string
}

func NewService(store port.FileStore, llm port.LLMClient, cfg domain.RAGConfig) *Service {
	return &Service{
		store:        store,
		llm:          llm,
		storageDir:   cfg.StorageDir,
		chunkSize:    cfg.ChunkSize,
		chunkOverlap: cfg.ChunkOverlap,
		topK:         cfg.TopK,
		// Embedding model adı artık config'ten gelmiyor: UI'dan seçilen model
		// MultiProviderClient üzerine pinlenir ve buradaki boş adı ezer.
		embeddingModel: "",
	}
}

func (s *Service) Upload(ctx context.Context, filename string, contentType string, reader io.Reader) (domain.FileRecord, error) {
	if err := os.MkdirAll(s.storageDir, 0755); err != nil {
		return domain.FileRecord{}, fmt.Errorf("create storage dir: %w", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return domain.FileRecord{}, fmt.Errorf("read file: %w", err)
	}

	rec, err := s.store.Create(ctx, filename, contentType, int64(len(data)))
	if err != nil {
		return domain.FileRecord{}, err
	}

	diskPath := filepath.Join(s.storageDir, rec.ID.String())
	if err := os.WriteFile(diskPath, data, 0644); err != nil {
		_ = s.store.Delete(ctx, rec.ID)
		return domain.FileRecord{}, fmt.Errorf("write file: %w", err)
	}

	text := string(data)
	chunks := chunkText(text, s.chunkSize, s.chunkOverlap)
	fileChunks := make([]domain.FileChunk, 0, len(chunks))
	for i, content := range chunks {
		emb, err := s.llm.Embed(ctx, content, s.embeddingModel)
		if err != nil {
			return domain.FileRecord{}, fmt.Errorf("embed chunk %d: %w", i, err)
		}
		fileChunks = append(fileChunks, domain.FileChunk{
			FileID:     rec.ID,
			ChunkIndex: i,
			Content:    content,
			Embedding:  emb,
		})
	}

	if err := s.store.SaveChunks(ctx, rec.ID, fileChunks); err != nil {
		_ = s.store.Delete(ctx, rec.ID)
		_ = os.Remove(diskPath)
		return domain.FileRecord{}, err
	}

	return rec, nil
}

func (s *Service) List(ctx context.Context) ([]domain.FileRecord, error) {
	return s.store.List(ctx)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(s.storageDir, id.String()))
	return nil
}

func (s *Service) InjectContext(ctx context.Context, messages []domain.Message, fileIDs []string) ([]domain.Message, error) {
	if len(fileIDs) == 0 {
		return messages, nil
	}

	ids := make([]uuid.UUID, 0, len(fileIDs))
	for _, fid := range fileIDs {
		id, err := uuid.Parse(fid)
		if err != nil {
			return messages, fmt.Errorf("invalid file_id: %s", fid)
		}
		ids = append(ids, id)
	}

	var query string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			query = messages[i].Content
			break
		}
	}
	if query == "" {
		return messages, nil
	}

	queryEmb, err := s.llm.Embed(ctx, query, s.embeddingModel)
	if err != nil {
		return messages, fmt.Errorf("embed query: %w", err)
	}

	chunks, err := s.store.SearchChunks(ctx, ids, queryEmb, s.topK)
	if err != nil {
		return messages, err
	}
	if len(chunks) == 0 {
		return messages, nil
	}

	var sb strings.Builder
	sb.WriteString("Relevant document excerpts:\n\n")
	for _, ch := range chunks {
		sb.WriteString(ch.Content)
		sb.WriteString("\n---\n")
	}

	systemMsg := domain.Message{
		Role:    domain.RoleSystem,
		Content: sb.String(),
	}
	return append([]domain.Message{systemMsg}, messages...), nil
}

func chunkText(text string, size, overlap int) []string {
	if size <= 0 {
		size = 1000
	}
	if overlap >= size {
		overlap = size / 4
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	var chunks []string
	for start := 0; start < len(runes); {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end >= len(runes) {
			break
		}
		start = end - overlap
	}
	return chunks
}
