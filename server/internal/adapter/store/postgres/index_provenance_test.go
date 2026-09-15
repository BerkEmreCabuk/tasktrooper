package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// staleEmbeddingRefusal is the gate every embedding search in this store passes
// through, so what it lets past is exactly what gets scored with cosine
// similarity. A wrong "not stale" here is not an empty result — it is a
// confident ranking of unrelated code, which nothing downstream can detect.
func TestStaleEmbeddingRefusal(t *testing.T) {
	cases := []struct {
		name string
		// the index's recorded provenance (migration 116)
		indexModel string
		indexDims  int
		// what the install embeds with now
		configuredModel string
		configuredDims  int
		// the length of the vector the query was embedded into
		queryDims int

		wantRefused bool
		wantPhrase  string
	}{
		{
			name:            "a fresh index is searched",
			indexModel:      "nomic-embed-text-v1.5",
			indexDims:       768,
			configuredModel: "nomic-embed-text-v1.5",
			configuredDims:  768,
			queryDims:       768,
			wantRefused:     false,
		},
		{
			name:            "an index built by another model is refused",
			indexModel:      "text-embedding-3-small",
			indexDims:       1536,
			configuredModel: "nomic-embed-text-v1.5",
			configuredDims:  768,
			queryDims:       768,
			wantRefused:     true,
			wantPhrase:      "text-embedding-3-small",
		},
		{
			// The pre-116 index: no model recorded, because the columns did not
			// exist when it was built. Unknown must never be read as matching.
			name:            "an index with no recorded model is refused once a model is configured",
			indexModel:      "",
			indexDims:       0,
			configuredModel: "nomic-embed-text-v1.5",
			configuredDims:  768,
			queryDims:       768,
			wantRefused:     true,
			wantPhrase:      "predates embedding provenance tracking",
		},
		{
			// Same pre-116 index on a deployment that has not chosen an
			// embedding provider: there is nothing to compare against, and
			// refusing here would break search for everyone who never changed
			// anything.
			name:            "an index with no recorded model is searched when nothing is configured",
			indexModel:      "",
			indexDims:       0,
			configuredModel: "",
			queryDims:       768,
			wantRefused:     false,
		},
		{
			// The arithmetic proof, with no configuration involved: the query
			// vector is a different length from the stored ones, which no two
			// runs of one model can produce. This is what still guards a
			// deployment whose resolver is not wired, or whose settings lookup
			// just failed.
			name:            "a query of another length is refused with no resolver at all",
			indexModel:      "nomic-embed-text-v1.5",
			indexDims:       768,
			configuredModel: "",
			queryDims:       1536,
			wantRefused:     true,
			wantPhrase:      "1536",
		},
		{
			// A provider that keeps the model name across a version that
			// changed the output size. The name alone would pass.
			name:            "the same name at a different size is refused",
			indexModel:      "embed-v1",
			indexDims:       768,
			configuredModel: "embed-v1",
			configuredDims:  1536,
			queryDims:       1536,
			wantRefused:     true,
		},
		{
			// An index whose dimension was never measured (a pass that embedded
			// nothing). Unknown size is not a mismatch; the model name is the
			// part that has actually been established.
			name:            "an unmeasured dimension is not a mismatch on its own",
			indexModel:      "nomic-embed-text-v1.5",
			indexDims:       0,
			configuredModel: "nomic-embed-text-v1.5",
			configuredDims:  768,
			queryDims:       768,
			wantRefused:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := staleEmbeddingRefusal(tc.indexModel, tc.indexDims, tc.configuredModel, tc.configuredDims, tc.queryDims)
			if !tc.wantRefused {
				if err != nil {
					t.Fatalf("a comparable index was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("an incomparable index was searched, which returns a confident wrong ranking rather than an error")
			}
			if !errors.Is(err, domain.ErrIndexEmbeddingStale) {
				// The caller has to tell this apart from "no index" and from a
				// database failure; a bare error cannot be told apart from
				// either.
				t.Fatalf("refusal is not recognisable as staleness: %v", err)
			}
			if tc.wantPhrase != "" && !strings.Contains(err.Error(), tc.wantPhrase) {
				t.Fatalf("refusal %q does not mention %q", err, tc.wantPhrase)
			}
			if !strings.Contains(err.Error(), "Re-index") {
				t.Fatalf("refusal %q does not say what fixes it", err)
			}
		})
	}
}
