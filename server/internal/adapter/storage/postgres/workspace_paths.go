package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

// workspacePathColumns are the columns that hold an absolute path into the
// workspace root. Left out on purpose: paths stored relative to a root
// (workspace_symbols/chunks/file_hashes.file_path, *.sub_project_path,
// *.root_directory, repository_profile_sections.source_paths), a CLI binary's
// install path (agent_cli_connection.binary_path), and history that records
// what a run saw at the time (session_steps.payload, audit_logs.result_preview).
var workspacePathColumns = []struct{ table, column string }{
	{"repositories", "root_path"},
	{"workspace_indexes", "root_path"},
	{"sessions", "workspace_dir"},
	{"sessions", "project_root"},
	{"task_agent_runs", "workspace_path"},
	{"agent_cli_connection", "catalog_path"},
}

// WorkspacePathStore rewrites stored workspace paths when the on-disk layout
// changes. It implements workspace.StoredPathRewriter.
type WorkspacePathStore struct {
	db *DB
}

func NewWorkspacePathStore(db *DB) *WorkspacePathStore {
	return &WorkspacePathStore{db: db}
}

// RewriteStoredPaths runs in one transaction. Rows are matched by value, so
// every row naming one path moves together. Each value is updated inside its
// own savepoint: a single update that fails (repositories.root_path is unique)
// is logged and skipped without undoing the others.
func (s *WorkspacePathStore) RewriteStoredPaths(ctx context.Context, markers []string, rewrite func(stored string) (string, bool)) (int, error) {
	if len(markers) == 0 {
		return 0, nil
	}
	total := 0
	err := s.db.InTx(ctx, func(tx pgx.Tx) error {
		total = 0
		for _, col := range workspacePathColumns {
			n, err := rewritePathColumn(ctx, tx, col.table, col.column, markers, rewrite)
			if err != nil {
				return err
			}
			total += n
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

func rewritePathColumn(ctx context.Context, tx pgx.Tx, table, column string, markers []string, rewrite func(string) (string, bool)) (int, error) {
	t := pgx.Identifier{table}.Sanitize()
	c := pgx.Identifier{column}.Sanitize()

	rows, err := tx.Query(ctx, fmt.Sprintf(
		`SELECT DISTINCT %[2]s FROM %[1]s
		 WHERE EXISTS (SELECT 1 FROM unnest($1::text[]) AS m(marker) WHERE strpos(%[2]s, m.marker) > 0)`,
		t, c), markers)
	if err != nil {
		return 0, fmt.Errorf("list %s.%s: %w", table, column, err)
	}
	stored, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("list %s.%s: %w", table, column, err)
	}

	update := fmt.Sprintf(`UPDATE %s SET %[2]s = $1 WHERE %[2]s = $2`, t, c)
	changed := 0
	for _, old := range stored {
		flat, ok := rewrite(old)
		if !ok || flat == old {
			continue
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			return changed, fmt.Errorf("savepoint for %s.%s: %w", table, column, err)
		}
		tag, err := sp.Exec(ctx, update, flat, old)
		if err != nil {
			_ = sp.Rollback(ctx)
			log.Warn().Err(err).Str("table", table).Str("column", column).Str("from", old).Str("to", flat).
				Msg("workspace layout: could not re-point a stored path; the row keeps its old value")
			continue
		}
		if err := sp.Commit(ctx); err != nil {
			return changed, fmt.Errorf("release savepoint for %s.%s: %w", table, column, err)
		}
		changed += int(tag.RowsAffected())
	}
	return changed, nil
}
