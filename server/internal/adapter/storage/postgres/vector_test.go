package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// pgvector reports every dimension conflict as a plain 22000 error, so the
// message text is the only thing that separates "your embedding model
// changed" from a real failure.
func TestVectorDimensionMismatchRecognizesPgvectorErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"insert into a narrower column", errors.New(`ERROR: expected 3072 dimensions, not 1024 (SQLSTATE 22000)`), true},
		{"comparing two dimensions", errors.New(`ERROR: different vector dimensions 1024 and 3072 (SQLSTATE 22000)`), true},
		{"unrelated failure", errors.New(`ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)`), false},
		{"no error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := vectorDimensionMismatch(tc.err); got != tc.want {
				t.Fatalf("vectorDimensionMismatch(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// Relaxing must drop the HNSW index before touching the column type (the index
// pins the dimension) and must clear the vectors left behind by the previous
// embedding model, otherwise every later search compares mixed dimensions.
func TestRelaxVectorDimensionStatementsOrderAndCleanup(t *testing.T) {
	stmts := relaxVectorDimensionStatements(1024)
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d: %v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "DROP INDEX") {
		t.Fatalf("the HNSW index must be dropped first, got %q", stmts[0])
	}
	if !strings.Contains(stmts[1], "TYPE vector ") {
		t.Fatalf("the column must become dimension-agnostic, got %q", stmts[1])
	}
	if !strings.Contains(stmts[2], "vector_dims(embedding_vec) <> 1024") {
		t.Fatalf("vectors of the previous model must be cleared, got %q", stmts[2])
	}
}

// Re-typing runs against a table that may still hold the previous model's
// vectors: they have to be cleared before the ALTER, or the cast fails and the
// bootstrap gives up on pgvector entirely.
func TestRetypeVectorStatementsClearForeignDimensionsBeforeAlter(t *testing.T) {
	stmts := retypeVectorStatements(1024)
	var alterAt, clearAt, indexAt = -1, -1, -1
	for i, s := range stmts {
		switch {
		case strings.Contains(s, "ALTER COLUMN embedding_vec TYPE vector(1024)"):
			alterAt = i
		case strings.Contains(s, "vector_dims(embedding_vec) <> 1024"):
			clearAt = i
		case strings.Contains(s, "CREATE INDEX"):
			indexAt = i
		}
	}
	if clearAt < 0 || alterAt < 0 || indexAt < 0 {
		t.Fatalf("missing clear/alter/index step: %v", stmts)
	}
	if clearAt > alterAt {
		t.Fatalf("foreign-dimension vectors must be cleared before the ALTER: %v", stmts)
	}
	if indexAt < alterAt {
		t.Fatalf("the HNSW index needs the typed column, so it must come last: %v", stmts)
	}
}

// A dimension change arrives mid-write: the insert fails once, the column is
// relaxed, and the same insert then succeeds. Without the retry the whole
// index run dies on the first chunk after switching embedding provider.
func TestExecWithVectorHealRetriesOnceAfterHealing(t *testing.T) {
	attempts, heals := 0, 0
	err := execWithVectorHeal(
		context.Background(),
		func(context.Context) error {
			attempts++
			if attempts == 1 {
				return errors.New("ERROR: expected 3072 dimensions, not 1024 (SQLSTATE 22000)")
			}
			return nil
		},
		func(context.Context) error { heals++; return nil },
	)
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if attempts != 2 || heals != 1 {
		t.Fatalf("expected 2 attempts and 1 heal, got %d attempts and %d heals", attempts, heals)
	}
}

func TestExecWithVectorHealLeavesUnrelatedErrorsAlone(t *testing.T) {
	attempts, heals := 0, 0
	want := errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)")
	err := execWithVectorHeal(
		context.Background(),
		func(context.Context) error { attempts++; return want },
		func(context.Context) error { heals++; return nil },
	)
	if !errors.Is(err, want) {
		t.Fatalf("expected the original error back, got %v", err)
	}
	if attempts != 1 || heals != 0 {
		t.Fatalf("expected no heal for an unrelated error, got %d attempts and %d heals", attempts, heals)
	}
}

// A failed heal must surface the original write error: reporting "could not
// drop index" hides the fact that the embedding dimension changed.
func TestExecWithVectorHealReportsBothWhenHealingFails(t *testing.T) {
	writeErr := errors.New("ERROR: expected 3072 dimensions, not 1024 (SQLSTATE 22000)")
	healErr := errors.New("permission denied for table workspace_chunks")
	err := execWithVectorHeal(
		context.Background(),
		func(context.Context) error { return writeErr },
		func(context.Context) error { return healErr },
	)
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected the write error to survive, got %v", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected the heal failure in the message, got %v", err)
	}
}
