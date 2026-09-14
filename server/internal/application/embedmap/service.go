package embedmap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Sources of points the map can be drawn from.
const (
	SourceFiles = "files"
	SourceCode  = "code"
)

// Request bounds. Out-of-range values are clamped rather than rejected: the map
// is a viewer control, and a slider that errors instead of saturating is worse
// than one that saturates.
const (
	DefaultLimit = 2000
	MinLimit     = 100
	MaxLimit     = 5000

	DefaultDims = 50
	MinDims     = 2
	MaxDims     = 128

	// snippetRunes is how much of a chunk travels with each point, for the
	// hover tooltip.
	snippetRunes = 200
)

var (
	// ErrInvalidSource is returned for a source other than files/code.
	ErrInvalidSource = errors.New(`source must be "files" or "code"`)
	// ErrRepositoryRequired is returned when source=code arrives without a
	// repository to read.
	ErrRepositoryRequired = errors.New("repository_id is required when source=code")
	// ErrUnavailable is returned when the store was never wired (no Postgres).
	ErrUnavailable = errors.New("embedding map unavailable")
)

// Service turns stored embeddings into a low-dimensional projection.
type Service struct {
	store port.EmbeddingMapStore
	// embeddings answers what this tenant embeds with now, so a source whose
	// index predates a model change can be labelled instead of silently
	// projected. Optional: unlike the indexer, this package has no configured
	// model of its own to fall back on, so with no resolver it labels nothing
	// and draws exactly what it drew before — a picture with no caveat, which
	// is what it was until this existed.
	embeddings port.EmbeddingProvenanceResolver
}

// New wires the service to its store.
func New(store port.EmbeddingMapStore) *Service {
	return &Service{store: store}
}

// SetEmbeddingResolver lets the map say when what it is drawing is not
// comparable with what the tenant embeds today.
//
// The map is the one consumer of these vectors that cannot simply refuse: it
// exists to show a person the shape of their corpus, and refusing would leave
// an empty panel with no explanation. So it draws and labels instead — but it
// must label, because a projection of two models' vectors is not a slightly
// worse picture, it is a picture of whichever subset survived buildMatrix's
// modal-length filter, arranged on axes derived from that subset alone.
//
// Wired late (platform/runtime), like every other consumer of this resolver.
func (s *Service) SetEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	if s == nil {
		return
	}
	s.embeddings = r
}

// resolvedEmbedding is the tenant's current model, or blank when nothing can
// answer. A resolver failure is not a verdict: it leaves the model blank, which
// domain.EmbeddingProvenanceStale reads as "nothing to compare against" and
// which therefore labels nothing, rather than marking every source stale
// because one settings lookup failed.
func (s *Service) resolvedEmbedding(ctx context.Context) (string, int) {
	if s == nil || s.embeddings == nil {
		return "", 0
	}
	model, dims, err := s.embeddings.ResolvedEmbedding(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("embedding map: could not resolve the configured embedding model; sources not checked for staleness")
		return "", 0
	}
	return model, dims
}

// Query is one map request, before clamping.
type Query struct {
	Source       string
	RepositoryID uuid.UUID
	Limit        int
	Dims         int
}

// FileSource describes the uploaded-document corpus.
type FileSource struct {
	Available     bool `json:"available"`
	ChunkCount    int  `json:"chunk_count"`
	DocumentCount int  `json:"document_count"`
}

// RepositorySource is one visualizable repository index.
type RepositorySource struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Branch     string     `json:"branch"`
	IndexID    string     `json:"index_id"`
	ChunkCount int        `json:"chunk_count"`
	FileCount  int        `json:"file_count"`
	IndexedAt  *time.Time `json:"indexed_at"`
	// EmbeddingModel is what produced this source's vectors, so the selector
	// can show it next to the chunk count rather than presenting every index as
	// interchangeable.
	EmbeddingModel string `json:"embedding_model,omitempty"`
	// EmbeddingStale marks a source whose vectors no longer match the tenant's
	// embedding model, and EmbeddingWarning is the sentence that says so. The
	// source stays listed and stays drawable — it is still the only picture
	// there is of that repository — but it is drawable with a caveat, not
	// silently.
	EmbeddingStale   bool   `json:"embedding_stale,omitempty"`
	EmbeddingWarning string `json:"embedding_warning,omitempty"`
}

// Sources is what the UI builds its selectors from.
type Sources struct {
	Files        FileSource         `json:"files"`
	Repositories []RepositorySource `json:"repositories"`
}

// Point is one chunk placed in the reduced space.
type Point struct {
	ID         string    `json:"id"`
	GroupID    string    `json:"group_id"`
	GroupLabel string    `json:"group_label"`
	ChunkIndex int       `json:"chunk_index"`
	Snippet    string    `json:"snippet"`
	Language   string    `json:"language"`
	Symbol     string    `json:"symbol"`
	Vector     []float64 `json:"vector"`
}

// Map is the projection handed to the browser, which runs UMAP over Vector.
type Map struct {
	Source       string  `json:"source"`
	RepositoryID string  `json:"repository_id"`
	Branch       string  `json:"branch"`
	Dimensions   int     `json:"dimensions"`
	Total        int     `json:"total"`
	Sampled      int     `json:"sampled"`
	Truncated    bool    `json:"truncated"`
	Points       []Point `json:"points"`
	// EmbeddingStale / EmbeddingWarning repeat the source's verdict on the map
	// itself, because the map is what a person is looking at when the cloud
	// looks wrong — and "half my repository vanished and the rest rearranged
	// itself" has exactly one honest explanation, which belongs on this
	// response and not only in the selector that led here.
	EmbeddingStale   bool   `json:"embedding_stale,omitempty"`
	EmbeddingWarning string `json:"embedding_warning,omitempty"`
}

// Sources lists what can be visualized.
func (s *Service) Sources(ctx context.Context) (Sources, error) {
	if s == nil || s.store == nil {
		return Sources{}, ErrUnavailable
	}
	stats, err := s.store.FileStats(ctx)
	if err != nil {
		return Sources{}, fmt.Errorf("embedding map file stats: %w", err)
	}
	repos, err := s.store.ListRepositorySources(ctx)
	if err != nil {
		return Sources{}, fmt.Errorf("embedding map repository sources: %w", err)
	}
	out := Sources{
		Files: FileSource{
			Available:     stats.ChunkCount > 0,
			ChunkCount:    stats.ChunkCount,
			DocumentCount: stats.DocumentCount,
		},
		Repositories: make([]RepositorySource, 0, len(repos)),
	}
	// Resolved once for the whole list, not once per repository: it is one
	// tenant-wide setting, and asking per row would put a settings read behind
	// every entry in a selector.
	configuredModel, configuredDims := s.resolvedEmbedding(ctx)
	for _, r := range repos {
		src := RepositorySource{
			ID:             r.RepositoryID.String(),
			Name:           r.Name,
			Branch:         r.Branch,
			IndexID:        r.IndexID.String(),
			ChunkCount:     r.ChunkCount,
			FileCount:      r.FileCount,
			IndexedAt:      r.IndexedAt,
			EmbeddingModel: r.EmbeddingModel,
		}
		if domain.EmbeddingProvenanceStale(r.EmbeddingModel, r.EmbeddingDims, configuredModel, configuredDims) {
			src.EmbeddingStale = true
			src.EmbeddingWarning = domain.EmbeddingStaleMessage(r.EmbeddingModel, r.EmbeddingDims, configuredModel, configuredDims)
		}
		out.Repositories = append(out.Repositories, src)
	}
	return out, nil
}

// Build samples the requested source, drops the vectors that cannot be
// projected, and reduces the rest with PCA.
func (s *Service) Build(ctx context.Context, q Query) (Map, error) {
	if s == nil || s.store == nil {
		return Map{}, ErrUnavailable
	}
	limit := clamp(q.Limit, MinLimit, MaxLimit, DefaultLimit)
	dims := clamp(q.Dims, MinDims, MaxDims, DefaultDims)

	switch q.Source {
	case SourceFiles:
		total, chunks, err := s.store.SampleFileChunks(ctx, limit)
		if err != nil {
			return Map{}, fmt.Errorf("sample file chunks: %w", err)
		}
		return project(SourceFiles, "", "", total, limit, dims, chunks), nil

	case SourceCode:
		if q.RepositoryID == uuid.Nil {
			return Map{}, ErrRepositoryRequired
		}
		repoID := q.RepositoryID.String()
		src, err := s.store.RepositorySource(ctx, q.RepositoryID)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				// Nothing indexed yet is an empty map, not a failure — the UI
				// shows "no points" the same way it does for an empty corpus.
				return emptyMap(SourceCode, repoID, "", dims), nil
			}
			return Map{}, fmt.Errorf("resolve repository index: %w", err)
		}
		total, chunks, err := s.store.SampleCodeChunks(ctx, src.IndexID, limit)
		if err != nil {
			return Map{}, fmt.Errorf("sample code chunks: %w", err)
		}
		out := project(SourceCode, repoID, src.Branch, total, limit, dims, chunks)
		// Labelled, not refused. Unlike a code search — where a wrong ranking
		// is indistinguishable from a right one and must therefore be blocked
		// (postgres.IndexStore.assertEmbeddingComparable) — the map's output is
		// looked at by a person, who can act on a warning. Refusing would leave
		// them with an empty panel and no idea why.
		configuredModel, configuredDims := s.resolvedEmbedding(ctx)
		if domain.EmbeddingProvenanceStale(src.EmbeddingModel, src.EmbeddingDims, configuredModel, configuredDims) {
			out.EmbeddingStale = true
			out.EmbeddingWarning = domain.EmbeddingStaleMessage(src.EmbeddingModel, src.EmbeddingDims, configuredModel, configuredDims)
		}
		return out, nil

	default:
		return Map{}, ErrInvalidSource
	}
}

func emptyMap(source, repositoryID, branch string, dims int) Map {
	return Map{
		Source:       source,
		RepositoryID: repositoryID,
		Branch:       branch,
		Dimensions:   dims,
		Points:       []Point{},
	}
}

// project runs the reduction and assembles the response.
//
// Dimensions is the length of every point's vector: the requested count, or the
// source's own dimension when the embeddings are shorter than that. Sampled is
// the number of points actually returned — a row whose stored embedding is
// unusable (zero, NaN/Inf, or of a different length than its neighbours) is
// dropped, so it can be lower than the number of rows read.
func project(source, repositoryID, branch string, total, limit, dims int, chunks []port.EmbeddingChunk) Map {
	out := emptyMap(source, repositoryID, branch, dims)
	out.Total = total
	out.Truncated = total > limit
	if len(chunks) == 0 {
		return out
	}

	vectors := make([][]float32, len(chunks))
	for i, ch := range chunks {
		vectors[i] = ch.Embedding
	}
	res := PCA(vectors, dims)
	if res.Dims > 0 {
		out.Dimensions = res.Dims
	}
	out.Points = make([]Point, 0, len(res.Kept))
	for i, idx := range res.Kept {
		ch := chunks[idx]
		out.Points = append(out.Points, Point{
			ID:         ch.ID,
			GroupID:    ch.GroupID,
			GroupLabel: ch.GroupLabel,
			ChunkIndex: ch.ChunkIndex,
			Snippet:    snippet(ch.Content),
			Language:   ch.Language,
			Symbol:     ch.Symbol,
			Vector:     res.Coords[i],
		})
	}
	out.Sampled = len(out.Points)
	return out
}

// snippet collapses every whitespace run to a single space and cuts the result
// to snippetRunes runes, so a tooltip never carries a chunk's indentation.
func snippet(content string) string {
	collapsed := strings.Join(strings.Fields(content), " ")
	runes := []rune(collapsed)
	if len(runes) > snippetRunes {
		return string(runes[:snippetRunes])
	}
	return collapsed
}

// clamp maps a request value into [min, max], substituting def when the caller
// sent nothing at all.
func clamp(value, min, max, def int) int {
	if value == 0 {
		return def
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
