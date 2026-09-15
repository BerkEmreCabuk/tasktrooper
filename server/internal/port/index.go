package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// EmbeddingProvenanceResolver answers "what model, and what vector length, do
// this install's embedding calls resolve to right now" — the right-hand side of
// domain.EmbeddingProvenanceStale.
//
// It is declared here, in port, rather than imported as a concrete type,
// because three layers need the same answer for three different jobs (the
// indexer stamping a pass, the store refusing a search, the embedding map
// labelling a source) and none of them may depend on the LLM-provider service.
// It is satisfied by *llmprovider.Service.ResolvedEmbedding.
//
// dimensions is 0 for a model whose size the resolver does not know. That is
// not a failure and must not be read as "zero-length vectors": a caller with a
// better source — a vector it just received — should prefer that, and
// domain.EmbeddingProvenanceStale ignores a zero on either side rather than
// treating it as a mismatch.
type EmbeddingProvenanceResolver interface {
	ResolvedEmbedding(ctx context.Context) (model string, dimensions int, err error)
}

type IndexStore interface {
	CreateIndex(ctx context.Context, sessionID uuid.UUID, rootPath string, treeText string) (domain.WorkspaceIndex, error)
	CreateProjectIndex(ctx context.Context, projectID uuid.UUID, rootPath, treeText string) (domain.WorkspaceIndex, error)
	GetIndexBySession(ctx context.Context, sessionID uuid.UUID) (domain.WorkspaceIndex, error)
	GetIndexByProject(ctx context.Context, projectID uuid.UUID) (domain.WorkspaceIndex, error)
	// Branch-aware indexes (branch "" is the repository default-branch index).
	GetIndexByProjectBranch(ctx context.Context, projectID uuid.UUID, branch string) (domain.WorkspaceIndex, error)
	CreateProjectBranchIndex(ctx context.Context, projectID uuid.UUID, branch, rootPath, treeText string) (domain.WorkspaceIndex, error)
	// CopyIndexData seeds a fresh branch index from the default-branch index so
	// only the branch's diff needs re-embedding.
	CopyIndexData(ctx context.Context, fromIndexID, toIndexID uuid.UUID) error
	// DeleteFileData removes chunks/symbols/edges/hashes of specific files for
	// incremental reindexing.
	DeleteFileData(ctx context.Context, indexID uuid.UUID, filePaths []string) error
	// DeleteFilesNotIn removes the derived rows of files that are no longer in
	// the tree. It is how a refresh drops deleted files without wiping the
	// index first — the wipe is what turned every interrupted pass into a
	// rebuild from zero.
	DeleteFilesNotIn(ctx context.Context, indexID uuid.UUID, keepPaths []string) error
	UpdateIndexTree(ctx context.Context, indexID uuid.UUID, rootPath, treeText string) error
	// UpdateIndexCommit stamps the workspace commit the index was built from so
	// query-time overlays can diff the live tree against it.
	UpdateIndexCommit(ctx context.Context, indexID uuid.UUID, commitSHA string) error
	// UpdateIndexEmbedding stamps what the pass ACTUALLY embedded with
	// (migration 116). It is written at the end of a pass, next to the commit,
	// because that is the moment both facts are known and true together: the
	// tree the vectors describe, and the model that produced them.
	//
	// It is not derived at read time from configuration, which is the whole
	// point — configuration is what MOVES. Only the pass itself knows which
	// model answered its embed calls, and dimensions is the length of a vector
	// it actually received wherever one was (0 means the pass embedded nothing
	// and had nothing to measure).
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
