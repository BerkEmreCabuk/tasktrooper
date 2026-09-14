package domain

import (
	"time"

	"github.com/google/uuid"
)

type FileRecord struct {
	ID          uuid.UUID `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

type FileChunk struct {
	ID         uuid.UUID `json:"id"`
	FileID     uuid.UUID `json:"file_id"`
	ChunkIndex int       `json:"chunk_index"`
	Content    string    `json:"content"`
	Embedding  []float32 `json:"embedding"`
}
