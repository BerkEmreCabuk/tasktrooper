package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EmbeddingChunk is one stored chunk with its raw embedding, read for the
// embedding-map projection. Content is already truncated by the store — the map
// only needs a tooltip-sized preview, never the whole chunk.
type EmbeddingChunk struct {
	ID         string
	GroupID    string
	GroupLabel string
	ChunkIndex int
	Content    string
	Language   string
	Symbol     string
	Embedding  []float32
}

// EmbeddingFileStats summarizes the uploaded-file corpus.
type EmbeddingFileStats struct {
	ChunkCount    int
	DocumentCount int
}

// EmbeddingRepositorySource is one repository that can be visualized, together
// with the workspace index the points would come from.
type EmbeddingRepositorySource struct {
	RepositoryID uuid.UUID
	Name         string
	Branch       string
	IndexID      uuid.UUID
	ChunkCount   int
	FileCount    int
	IndexedAt    *time.Time
	// EmbeddingModel and EmbeddingDims are the index's recorded provenance
	// (migration 116), carried through so the map can say WHY a repository's
	// points are not worth reading rather than drawing them anyway.
	//
	// A projection over vectors from two models is not merely inaccurate, it is
	// arbitrary: PCA drops every row whose length differs from the modal one
	// (embedmap.buildMatrix), so a model change silently halves the cloud and
	// rearranges what is left into axes derived from whichever half won.
	EmbeddingModel string
	EmbeddingDims  int
}

// EmbeddingMapStore reads chunk embeddings for the 2D map. Sampling is pushed
// into SQL: a large repository has six figures of JSONB embeddings and only a
// couple of thousand of them ever reach the browser.
type EmbeddingMapStore interface {
	// FileStats counts the uploaded-file chunks and the documents they came from.
	FileStats(ctx context.Context) (EmbeddingFileStats, error)
	// ListRepositorySources returns one row per repository that has an index
	// with at least one chunk.
	ListRepositorySources(ctx context.Context) ([]EmbeddingRepositorySource, error)
	// RepositorySource resolves the index a repository's map is drawn from.
	// Returns ErrNotFound when the repository has no index with chunks.
	RepositorySource(ctx context.Context, repositoryID uuid.UUID) (EmbeddingRepositorySource, error)
	// SampleFileChunks returns the total chunk count and up to limit chunks
	// sampled evenly across a stable ordering.
	SampleFileChunks(ctx context.Context, limit int) (total int, chunks []EmbeddingChunk, err error)
	// SampleCodeChunks does the same for one workspace index.
	SampleCodeChunks(ctx context.Context, indexID uuid.UUID, limit int) (total int, chunks []EmbeddingChunk, err error)
}
