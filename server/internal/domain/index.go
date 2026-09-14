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
	// up to date before the last pass — the pull from origin failed, or the
	// cloud's own mirror clone was gone and could not be restored at all. It is
	// runtime state, not a stored column: a completed index built on a stale (or
	// absent) clone is otherwise indistinguishable from one built on current
	// code.
	SyncWarning string `json:"sync_warning,omitempty"`
	// EmbeddingModel and EmbeddingDims are what this index was ACTUALLY built
	// with (migration 116), written by the pass that embedded it rather than
	// inferred afterwards. They are the left-hand side of
	// EmbeddingProvenanceStale; the right-hand side is whatever the tenant's
	// embedding provider resolves to now.
	//
	// Both are blank/zero on an index built before migration 116, and "unknown"
	// is never read as "matches" — see EmbeddingProvenanceStale.
	EmbeddingModel string `json:"embedding_model,omitempty"`
	EmbeddingDims  int    `json:"embedding_dims,omitempty"`
	// EmbeddingStale reports that this index's vectors are not comparable with a
	// query embedded by the tenant's CURRENT model, so search against it is
	// refused until it has been rebuilt.
	//
	// Runtime state, like SyncWarning, and for the reason migration 116 stored
	// no boolean: staleness is a comparison against a setting that moves
	// independently of any index row, so a persisted flag would be wrong from
	// the moment the setting changed until some sweep caught up. It is computed
	// on every read instead.
	//
	// It is a field at all because of what its absence looks like: a green
	// "completed 100%" card over an index whose every vector is unusable, and a
	// code search that answers "no results" for a repository that is fully
	// indexed.
	EmbeddingStale bool `json:"embedding_stale,omitempty"`
	// EmbeddingWarning is the sentence shown next to that flag: what the index
	// holds, what is configured now, and that a re-index is the fix. Empty when
	// EmbeddingStale is false.
	EmbeddingWarning string `json:"embedding_warning,omitempty"`
}

// ErrIndexEmbeddingStale marks every refusal caused by searching an index whose
// vectors came from a different embedding model than the one the query would be
// embedded with.
//
// It is a sentinel because the alternative to refusing is not an empty result,
// it is a WRONG one: cosine similarity between two models' vectors does not
// error, it returns a confident ranking of unrelated code. Callers have to tell
// that apart from "this repository has no index" and from a transient database
// failure — the first is fixed by re-indexing, the second by indexing, the third
// by retrying — and only a typed error lets them, so the sentence can say which
// one it is instead of every caller inventing "no results".
var ErrIndexEmbeddingStale = errors.New("workspace index was built with a different embedding model")

// EmbeddingStaleMessage is the one sentence every layer says about a stale
// index — the tool result an agent reads, the API error the settings page
// renders, the warning on the index card — so the same fact cannot acquire
// three different explanations.
//
// It names both models on purpose. "Re-index this repository" on its own reads
// like a defect report; naming what changed makes it plain that the trigger was
// a deliberate embedding-provider change and that nothing is broken.
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
	// compare against, and "a different model" on its own would read as a
	// guess rather than as the measurement it is.
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
