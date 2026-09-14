package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// VectorCapabilities reports which optional Postgres extensions are usable.
type VectorCapabilities struct {
	Vector bool
	Trgm   bool
}

func DetectVectorCapabilities(ctx context.Context, db *DB) VectorCapabilities {
	caps := VectorCapabilities{}
	// pg_extension is a catalog table, identical for every tenant and covered
	// by no policy, so this is one of the two reads in this package that
	// deliberately does not open a tenant transaction.
	rows, err := db.schemaPool().Query(ctx, `SELECT extname FROM pg_extension WHERE extname IN ('vector','pg_trgm')`)
	if err != nil {
		return caps
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		switch name {
		case "vector":
			caps.Vector = true
		case "pg_trgm":
			caps.Trgm = true
		}
	}
	return caps
}

// workspaceVectorIndex is the HNSW index over workspace_chunks.embedding_vec.
// It pins the column's dimension, so it has to go before any re-typing.
const workspaceVectorIndex = "idx_workspace_chunks_embedding_hnsw"

// BootstrapWorkspaceVectors types the embedding_vec column to the dominant
// embedding dimension, backfills it from the JSONB column, and creates an HNSW
// index. Idempotent and non-fatal: any failure just leaves the in-Go cosine
// fallback active.
//
// The dimension is derived from the most common embedding rather than an
// arbitrary row: after an embedding-model change the table holds both the old
// and the new model's vectors for as long as the reindex is running, and
// typing the column to the dying model would reject every new write.
// The dimension question is now asked of the WHOLE database, not of one
// tenant: embedding_vec is one column shared by every tenant's chunks, so its
// type cannot be a per-tenant decision. workspace_chunks is policy-protected,
// so the probe runs once per known tenant and the winner is the dimension with
// the most rows across the fleet.
func BootstrapWorkspaceVectors(ctx context.Context, db *DB) bool {
	dim := dominantEmbeddingDimension(ctx, db)
	if dim <= 0 {
		log.Info().Msg("pgvector bootstrap skipped: no embeddings yet to derive dimension")
		return false
	}
	for _, stmt := range retypeVectorStatements(dim) {
		// DDL, so the schema pool: re-typing a column and building an index are
		// not writes to anybody's rows.
		if _, err := db.schemaPool().Exec(ctx, stmt); err != nil {
			log.Warn().Err(err).Msg("pgvector bootstrap step failed, in-Go cosine fallback stays active")
			return false
		}
	}
	log.Info().Int("dim", dim).Msg("pgvector workspace chunk search enabled")
	return true
}

// dominantEmbeddingDimension counts embeddings per dimension across every
// tenant this database serves and returns the most common one, or 0 when there
// are none anywhere.
func dominantEmbeddingDimension(ctx context.Context, db *DB) int {
	ids, err := db.Tenants(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("pgvector bootstrap could not list tenants")
		return 0
	}
	counts := map[int]int64{}
	for _, id := range ids {
		scoped := tenant.With(ctx, tenant.Identity{TenantID: id, Role: tenant.RoleMember})
		rows, err := db.Query(scoped, `
			SELECT jsonb_array_length(embedding) AS dim, count(*)
			FROM workspace_chunks
			WHERE embedding IS NOT NULL AND jsonb_typeof(embedding) = 'array'
			GROUP BY dim
		`)
		if err != nil {
			continue
		}
		for rows.Next() {
			var dim int
			var n int64
			if rows.Scan(&dim, &n) == nil && dim > 0 {
				counts[dim] += n
			}
		}
		rows.Close()
	}
	best, bestN := 0, int64(0)
	for dim, n := range counts {
		// Ties break on the larger dimension, matching the previous ORDER BY.
		if n > bestN || (n == bestN && dim > best) {
			best, bestN = dim, n
		}
	}
	return best
}

// retypeVectorStatements re-pins embedding_vec to dim. Vectors of any other
// dimension are dropped first: they are leftovers of a previous embedding
// model, they cannot be cast, and keeping them would break every comparison.
func retypeVectorStatements(dim int) []string {
	return []string{
		fmt.Sprintf(`DROP INDEX IF EXISTS %s`, workspaceVectorIndex),
		fmt.Sprintf(`UPDATE workspace_chunks SET embedding_vec = NULL
			WHERE embedding_vec IS NOT NULL AND vector_dims(embedding_vec) <> %d`, dim),
		fmt.Sprintf(`ALTER TABLE workspace_chunks ALTER COLUMN embedding_vec TYPE vector(%d) USING embedding_vec::vector(%d)`, dim, dim),
		fmt.Sprintf(`UPDATE workspace_chunks SET embedding_vec = embedding::text::vector(%d)
			WHERE embedding_vec IS NULL AND embedding IS NOT NULL AND jsonb_array_length(embedding) = %d`, dim, dim),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s
			ON workspace_chunks USING hnsw (embedding_vec vector_cosine_ops)`, workspaceVectorIndex),
	}
}

// relaxVectorDimensionStatements make embedding_vec accept any dimension. Used
// when a write is rejected because the embedding model changed under a typed
// column: the new vectors must land now, and the next bootstrap re-pins the
// column (and rebuilds the HNSW index) around them.
func relaxVectorDimensionStatements(dim int) []string {
	return []string{
		fmt.Sprintf(`DROP INDEX IF EXISTS %s`, workspaceVectorIndex),
		`ALTER TABLE workspace_chunks ALTER COLUMN embedding_vec TYPE vector USING embedding_vec::vector`,
		fmt.Sprintf(`UPDATE workspace_chunks SET embedding_vec = NULL
			WHERE embedding_vec IS NOT NULL AND vector_dims(embedding_vec) <> %d`, dim),
	}
}

// vectorDimensionMismatch reports whether err is pgvector refusing a vector
// whose dimension differs from the column's (on write) or from the vector it
// is compared with (on search). Both are plain 22000 errors, so the message is
// the only signal.
func vectorDimensionMismatch(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "dimensions, not") || strings.Contains(msg, "different vector dimensions")
}

// vectorHealer serializes column-wide repairs so parallel index workers hitting
// the same dimension change repair it once instead of racing each other.
type vectorHealer struct {
	mu sync.Mutex
}

func (h *vectorHealer) relax(ctx context.Context, pool *DB, dim int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, stmt := range relaxVectorDimensionStatements(dim) {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	log.Info().Int("dim", dim).Msg("embedding dimension changed: vector column relaxed, HNSW index rebuilt on next start")
	return nil
}

// execWithVectorHeal runs exec and, if it fails only because the embedding
// dimension changed, heals the column and runs it once more. Any other error
// is returned untouched.
func execWithVectorHeal(ctx context.Context, exec, heal func(context.Context) error) error {
	err := exec(ctx)
	if !vectorDimensionMismatch(err) {
		return err
	}
	if healErr := heal(ctx); healErr != nil {
		return fmt.Errorf("%w (recovering from the dimension change also failed: %v)", err, healErr)
	}
	return exec(ctx)
}

func vectorLiteral(embedding []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range embedding {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
