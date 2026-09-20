package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type FileStore struct {
	pool *DB
}

func NewFileStore(pool *DB) *FileStore {
	return &FileStore{pool: pool}
}

func (f *FileStore) Create(ctx context.Context, filename, contentType string, sizeBytes int64) (domain.FileRecord, error) {
	var rec domain.FileRecord
	err := f.pool.QueryRow(ctx, `
		INSERT INTO files (filename, content_type, size_bytes)
		VALUES ($1, $2, $3)
		RETURNING id, filename, content_type, size_bytes, created_at
	`, filename, contentType, sizeBytes).Scan(&rec.ID, &rec.Filename, &rec.ContentType, &rec.SizeBytes, &rec.CreatedAt)
	if err != nil {
		return domain.FileRecord{}, fmt.Errorf("create file: %w", err)
	}
	return rec, nil
}

func (f *FileStore) Get(ctx context.Context, id uuid.UUID) (domain.FileRecord, error) {
	var rec domain.FileRecord
	err := f.pool.QueryRow(ctx, `
		SELECT id, filename, content_type, size_bytes, created_at FROM files WHERE id = $1
	`, id).Scan(&rec.ID, &rec.Filename, &rec.ContentType, &rec.SizeBytes, &rec.CreatedAt)
	if err != nil {
		return domain.FileRecord{}, fmt.Errorf("get file: %w", err)
	}
	return rec, nil
}

func (f *FileStore) List(ctx context.Context) ([]domain.FileRecord, error) {
	rows, err := f.pool.Query(ctx, `
		SELECT id, filename, content_type, size_bytes, created_at FROM files ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()

	var files []domain.FileRecord
	for rows.Next() {
		var rec domain.FileRecord
		if err := rows.Scan(&rec.ID, &rec.Filename, &rec.ContentType, &rec.SizeBytes, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, rec)
	}
	return files, rows.Err()
}

func (f *FileStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := f.pool.Exec(ctx, `DELETE FROM files WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("file not found")
	}
	return nil
}

func (f *FileStore) SaveChunks(ctx context.Context, fileID uuid.UUID, chunks []domain.FileChunk) error {
	for _, ch := range chunks {
		emb, err := json.Marshal(ch.Embedding)
		if err != nil {
			return fmt.Errorf("marshal embedding: %w", err)
		}
		_, err = f.pool.Exec(ctx, `
			INSERT INTO file_chunks (file_id, chunk_index, content, embedding)
			VALUES ($1, $2, $3, $4)
		`, fileID, ch.ChunkIndex, ch.Content, emb)
		if err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
	}
	return nil
}

type scoredChunk struct {
	chunk domain.FileChunk
	score float64
}

func (f *FileStore) SearchChunks(ctx context.Context, fileIDs []uuid.UUID, queryEmbedding []float32, topK int) ([]domain.FileChunk, error) {
	if len(fileIDs) == 0 || len(queryEmbedding) == 0 {
		return nil, nil
	}

	rows, err := f.pool.Query(ctx, `
		SELECT id, file_id, chunk_index, content, embedding
		FROM file_chunks WHERE file_id = ANY($1)
	`, fileIDs)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()

	var scored []scoredChunk
	for rows.Next() {
		var ch domain.FileChunk
		var embJSON []byte
		if err := rows.Scan(&ch.ID, &ch.FileID, &ch.ChunkIndex, &ch.Content, &embJSON); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		if err := json.Unmarshal(embJSON, &ch.Embedding); err != nil {
			continue
		}
		scored = append(scored, scoredChunk{chunk: ch, score: cosineSimilarity(queryEmbedding, ch.Embedding)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if topK > len(scored) {
		topK = len(scored)
	}
	result := make([]domain.FileChunk, 0, topK)
	for i := 0; i < topK; i++ {
		result = append(result, scored[i].chunk)
	}
	return result, nil
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
