package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// EmbeddingProvenanceResolver answers "what model, and what vector length, do
// this install's embedding calls resolve to right now". Declared here because
// three layers need the same answer and none may depend on the LLM-provider
// service. dimensions is 0 for a model whose size the resolver does not know —
// not a failure, and ignored by EmbeddingProvenanceStale rather than read as
// a mismatch.
type EmbeddingProvenanceResolver interface {
	ResolvedEmbedding(ctx context.Context) (model string, dimensions int, err error)
}

type IndexStore interface {
	CreateIndex(ctx context.Context, sessionID uuid.UUID, rootPath string, treeText string) (domain.WorkspaceIndex, error)
	CreateProjectIndex(ctx context.Context, projectID uuid.UUID, rootPath, treeText string) (domain.WorkspaceIndex, error)
	GetIndexBySession(ctx context.Context, sessionID uuid.UUID) (domain.WorkspaceIndex, error)
	GetIndexByProject(ctx context.Context, projectID uuid.UUID) (domain.WorkspaceIndex, error)
	// branch "" is the repository default-branch index.
	GetIndexByProjectBranch(ctx context.Context, projectID uuid.UUID, branch string) (domain.WorkspaceIndex, error)
	CreateProjectBranchIndex(ctx context.Context, projectID uuid.UUID, branch, rootPath, treeText string) (domain.WorkspaceIndex, error)
	// Seeds a fresh branch index from the default-branch index so only the
	// branch's diff needs re-embedding.
	CopyIndexData(ctx context.Context, fromIndexID, toIndexID uuid.UUID) error
	DeleteFileData(ctx context.Context, indexID uuid.UUID, filePaths []string) error
	// Drops derived rows of files no longer in the tree; the wipe-first
	// alternative is what turned every interrupted pass into a rebuild.
	DeleteFilesNotIn(ctx context.Context, indexID uuid.UUID, keepPaths []string) error
	UpdateIndexTree(ctx context.Context, indexID uuid.UUID, rootPath, treeText string) error
	UpdateIndexCommit(ctx context.Context, indexID uuid.UUID, commitSHA string) error
	// Stamps what the pass ACTUALLY embedded with, at the end of the pass next
	// to the commit; not derived at read time from configuration, because
	// configuration is what MOVES. dimensions is 0 when the pass embedded
	// nothing.
	UpdateIndexEmbedding(ctx context.Context, indexID uuid.UUID, model string, dimensions int) error
	CountIndexData(ctx context.Context, indexID uuid.UUID) (chunks, symbols int, err error)
	UpdateIndexStatus(ctx context.Context, indexID uuid.UUID, status domain.IndexStatus, fileCount, chunkCount, symbolCount int, errMsg string) error
	UpdateIndexProgress(ctx context.Context, indexID uuid.UUID, filesProcessed, filesTotal int) error
	SaveSymbols(ctx context.Context, indexID uuid.UUID, symbols []domain.WorkspaceSymbol) error
	SaveChunks(ctx context.Context, indexID uuid.UUID, chunks []domain.WorkspaceChunk) error
	SaveEdges(ctx context.Context, indexID uuid.UUID, edges []domain.WorkspaceEdge) error
	SaveFileHashes(ctx context.Context, indexID uuid.UUID, hashes []domain.WorkspaceFileHash) error
	GetFileHashes(ctx context.Context, indexID uuid.UUID) (map[string]string, error)
	SearchChunks(ctx context.Context, sessionID uuid.UUID, embedding []float32, topK int) ([]domain.WorkspaceChunk, error)
	SearchChunksByIndex(ctx context.Context, indexID uuid.UUID, embedding []float32, topK int) ([]domain.WorkspaceChunk, error)
	SearchChunksHybrid(ctx context.Context, indexID uuid.UUID, rawQuery string, embedding []float32, topK int) ([]domain.WorkspaceChunk, error)
	SearchSymbols(ctx context.Context, name string, indexID uuid.UUID) ([]domain.WorkspaceSymbol, error)
	GetChunkBySymbol(ctx context.Context, indexID uuid.UUID, filePath, symbolName string) (domain.WorkspaceChunk, error)
	ListEdges(ctx context.Context, indexID uuid.UUID) ([]domain.WorkspaceEdge, error)
	DeleteIndexData(ctx context.Context, indexID uuid.UUID) error
}
