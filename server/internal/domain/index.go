package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type IndexStatus string

const (
	IndexStatusPending   IndexStatus = "pending"
	IndexStatusRunning   IndexStatus = "running"
	IndexStatusCompleted IndexStatus = "completed"
	IndexStatusFailed    IndexStatus = "failed"
)

type WorkspaceIndex struct {
	ID             uuid.UUID   `json:"id"`
	SessionID      *uuid.UUID  `json:"session_id,omitempty"`
	ProjectID      *uuid.UUID  `json:"project_id,omitempty"`
	Branch         string      `json:"branch,omitempty"`
	CommitSHA      string      `json:"commit_sha,omitempty"`
	RootPath       string      `json:"root_path"`
	Status         IndexStatus `json:"status"`
	FileCount      int         `json:"file_count"`
	ChunkCount     int         `json:"chunk_count"`
	SymbolCount    int         `json:"symbol_count"`
	FilesTotal     int         `json:"files_total"`
	FilesProcessed int         `json:"files_processed"`
	TreeText       string      `json:"tree_text"`
	IndexedAt      *time.Time  `json:"indexed_at,omitempty"`
	Error          string      `json:"error,omitempty"`
	// SyncWarning reports why the code this index describes could not be brought
	// up to date before the last pass (the pull failed, or the mirror clone was
	// gone); runtime state, not a stored column — a completed index built on a
	// stale clone is otherwise indistinguishable from one built on current code.
	SyncWarning string `json:"sync_warning,omitempty"`
	// EmbeddingModel and EmbeddingDims are what this index was ACTUALLY built
	// with (migration 116), the left-hand side of EmbeddingProvenanceStale; the
	// right-hand side is whatever this install's embedding provider resolves to
	// now. Both are blank/zero on an index built before the migration, and
	// "unknown" is never read as "matches".
	EmbeddingModel string `json:"embedding_model,omitempty"`
	EmbeddingDims  int    `json:"embedding_dims,omitempty"`
	// EmbeddingStale reports that this index's vectors are not comparable with
	// a query embedded by the CURRENT model, so search against it is refused
	// until it has been rebuilt. Runtime state, like SyncWarning, computed on
	// every read: staleness is a comparison against a setting that moves
	// independently of any index row, so a persisted flag would be wrong from
	// the moment the setting changed. It exists because of what its absence
	// looks like: a green "completed 100%" card over an index whose every
	// vector is unusable, and a search that answers "no results".
	EmbeddingStale bool `json:"embedding_stale,omitempty"`
	// EmbeddingWarning is the sentence shown next to that flag: what the index
	// holds, what is configured now, and that a re-index is the fix. Empty when
	// EmbeddingStale is false.
	EmbeddingWarning string `json:"embedding_warning,omitempty"`
}

// ErrIndexEmbeddingStale marks every refusal caused by searching an index whose
// vectors came from a different embedding model than the query would be
// embedded with. It is a sentinel because the alternative to refusing is not an
// empty result, it is a WRONG one: cosine similarity between two models'
// vectors does not error, it returns a confident ranking of unrelated code.
// Callers need a typed error to tell that apart from "no index" and from a
// transient failure.
var ErrIndexEmbeddingStale = errors.New("workspace index was built with a different embedding model")

// EmbeddingStaleMessage is the one sentence every layer says about a stale
// index — tool result, API error, card warning — so the same fact cannot
// acquire three different explanations. It names both models on purpose:
// "re-index this repository" alone reads like a defect report; naming what
// changed makes it plain the trigger was a deliberate embedding-provider change.
func EmbeddingStaleMessage(indexModel string, indexDimensions int, configuredModel string, configuredDimensions int) string {
	built := indexModel
	if built == "" {
		built = "an unrecorded model (it predates embedding provenance tracking)"
	} else if indexDimensions > 0 {
		built = fmt.Sprintf("%s (%d dimensions)", built, indexDimensions)
	}
	// The dimension is appended even when the model cannot be named, because
	// that is the case where it is the ONLY evidence: a query vector of a
	// different length is proof of a model change with nothing configured to
	// compare against.
	now := configuredModel
	if now == "" {
		now = "a different model"
	}
	if configuredDimensions > 0 {
		now = fmt.Sprintf("%s (%d dimensions)", now, configuredDimensions)
	}
	return fmt.Sprintf("this index was built with %s but embeddings are now produced by %s; vectors from two models are not "+
		"comparable, so searching it would return a confident wrong ranking rather than an error. Re-index this repository", built, now)
}

func (idx WorkspaceIndex) ProgressPercent() int {
	if idx.FilesTotal <= 0 {
		if idx.Status == IndexStatusCompleted {
			return 100
		}
		return 0
	}
	pct := idx.FilesProcessed * 100 / idx.FilesTotal
	if pct > 100 {
		return 100
	}
	return pct
}

type WorkspaceChunk struct {
	ID         uuid.UUID `json:"id"`
	IndexID    uuid.UUID `json:"index_id"`
	FilePath   string    `json:"file_path"`
	SymbolName string    `json:"symbol_name"`
	Kind       string    `json:"kind"`
	StartLine  int       `json:"start_line"`
	EndLine    int       `json:"end_line"`
	Language   string    `json:"language"`
	Signature  string    `json:"signature"`
	Content    string    `json:"content"`
	Embedding  []float32 `json:"embedding,omitempty"`
	Score      float64   `json:"score,omitempty"`
}

type WorkspaceSymbol struct {
	ID        uuid.UUID `json:"id"`
	IndexID   uuid.UUID `json:"index_id"`
	FilePath  string    `json:"file_path"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Signature string    `json:"signature"`
	Doc       string    `json:"doc"`
	StartLine int       `json:"start_line"`
	EndLine   int       `json:"end_line"`
}

type WorkspaceEdge struct {
	IndexID    uuid.UUID `json:"index_id"`
	FromFile   string    `json:"from_file"`
	FromSymbol string    `json:"from_symbol"`
	ToFile     string    `json:"to_file"`
	ToSymbol   string    `json:"to_symbol"`
	EdgeKind   string    `json:"edge_kind"`
}

type WorkspaceFileHash struct {
	IndexID  uuid.UUID `json:"index_id"`
	FilePath string    `json:"file_path"`
	Hash     string    `json:"hash"`
}

type IndexSearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

type InjectOptions struct {
	TopK            int
	IncludeTree     bool
	IncludeSkeleton bool
	MaxChunkTokens  int
	TargetSymbol    string
	TargetFilePath  string
	ExpandGraph     bool
	// RewriteQuery turns the query text into up to two extra LLM-written
	// code-search queries before embedding. Only worth a model call when the
	// query is raw human prose (chat sessions). Board runs and orchestration
	// subtasks leave it false: their trigger text is already LLM-authored and
	// carries exact file/interface names from the task's technical description.
	RewriteQuery bool
}
