package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/migrations"
)

// migrationAdvisoryLockKey serialises a whole migration run across processes.
// The sibling of schemaAdvisoryLockKey (5212026001) lives in another database,
// but keeping them adjacent means one grep finds both; never reuse this value.
const migrationAdvisoryLockKey int64 = 5212026002

const (
	// migrationLockTimeout bounds a table-lock wait: lock queues are FIFO, so a
	// migration queued behind one long transaction blocks later queries too and
	// an unbounded wait would delay HTTP server startup. 3s rides out ordinary
	// short transactions while the whole retry ladder below still fits a boot.
	migrationLockTimeout = "3s"

	// migrationStatementTimeout bounds a locked statement; generous for
	// backfills, tight enough to fail a runaway migration loudly instead of
	// pinning locks forever.
	migrationStatementTimeout = "5min"

	// advisoryLockTimeout bounds one pg_advisory_lock wait (lock_timeout covers
	// it on Postgres 16); longer than migrationLockTimeout because the holder is
	// another pod working the whole pending set.
	advisoryLockTimeout = "30s"

	// migrationLockAttempts: with the backoff below the worst case is ~22s
	// before the pod gives up and restarts.
	migrationLockAttempts = 5

	// advisoryLockAttempts x advisoryLockTimeout is the ~5min budget for waiting
	// on a peer pod; failing loudly beats hanging silently.
	advisoryLockAttempts = 10
)

// retryBaseDelay is a var purely so tests can collapse the retry ladder.
var retryBaseDelay = 250 * time.Millisecond

// pgLockNotAvailable is SQLSTATE 55P03, raised when lock_timeout fires.
const pgLockNotAvailable = "55P03"

// migrationConn is the pool subset the runner needs, so retries can be tested
// without a live database.
type migrationConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// RunMigrations applies every pending migration once under a database-wide
// advisory lock: two processes can migrate at once (a server starting while the
// previous one shuts down), and the loser of that race used to fail on the
// INSERT with 23505, which cmd/agent-server turns into log.Fatal.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	return runMigrationsPool(ctx, pool, "")
}

// RunMigrationsUpTo is RunMigrations stopping after version, so a test can stand
// an older schema and then apply one migration to populated data.
func RunMigrationsUpTo(ctx context.Context, pool *pgxpool.Pool, version string) error {
	names, err := listMigrationFiles(migrations.Up)
	if err != nil {
		return err
	}
	for _, name := range names {
		if migrationVersion(name) == version {
			return runMigrationsPool(ctx, pool, version)
		}
	}
	return fmt.Errorf("no migration named %q", version)
}

func runMigrationsPool(ctx context.Context, pool *pgxpool.Pool, upTo string) error {
	// The advisory lock is session-scoped, so locking through the pool would
	// guard nothing — one session takes the lock, another runs the DDL. Pinning
	// to one connection also means a run cannot deadlock a small pool.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	return runMigrationsThrough(ctx, conn, func() {
		// Closing destroys the connection instead of pooling it; Postgres frees
		// session locks when the backend dies — the escape hatch when unlock fails.
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
	}, upTo)
}

// runMigrationsOn is RunMigrations minus the pool bookkeeping: the lock is held
// over the whole run on the single pinned connection. discard retires a
// connection of unknown lock state when the unlock fails.
func runMigrationsOn(ctx context.Context, conn migrationConn, discard func()) error {
	return runMigrationsThrough(ctx, conn, discard, "")
}

// runMigrationsThrough is runMigrationsOn applying nothing past upTo ("" = all).
func runMigrationsThrough(ctx context.Context, conn migrationConn, discard func(), upTo string) error {
	if err := acquireMigrationLock(ctx, conn); err != nil {
		return err
	}
	// Unlock must run on every path: leaving it held would stall the next pod.
	defer releaseMigrationLock(ctx, conn, discard)

	return runMigrationsLocked(ctx, conn, upTo)
}

// runMigrationsLocked is the body of a run; callers already hold the lock on conn.
func runMigrationsLocked(ctx context.Context, conn migrationConn, upTo string) error {
	// ensureSchemaTable sits inside the lock: CREATE TABLE IF NOT EXISTS is not
	// race-safe, concurrent identical DDL raises 23505.
	if err := ensureSchemaTable(ctx, conn); err != nil {
		return err
	}
	applied, err := loadApplied(ctx, conn)
	if err != nil {
		return err
	}
	names, err := listMigrationFiles(migrations.Up)
	if err != nil {
		return err
	}
	for _, name := range names {
		version := migrationVersion(name)
		if upTo != "" && version > upTo {
			break
		}
		if applied[version] {
			continue
		}
		body, err := fs.ReadFile(migrations.Up, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := applyMigration(ctx, conn, name, version, string(body)); err != nil {
			return err
		}
	}
	return nil
}

// acquireMigrationLock takes the run-level advisory lock, a bounded number of
// bounded attempts while any peer pod finishes.
func acquireMigrationLock(ctx context.Context, conn migrationConn) error {
	// Session-scoped so lock_timeout also covers the pg_advisory_lock call.
	if _, err := conn.Exec(ctx, `SELECT set_config('lock_timeout', $1, false)`, advisoryLockTimeout); err != nil {
		return fmt.Errorf("set advisory lock_timeout: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= advisoryLockAttempts; attempt++ {
		_, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockKey)
		if err == nil {
			// Hand the connection back to the per-migration SET LOCAL regime.
			if _, err := conn.Exec(ctx, `RESET lock_timeout`); err != nil {
				return fmt.Errorf("reset lock_timeout: %w", err)
			}
			return nil
		}
		lastErr = err
		if !isLockNotAvailable(err) {
			return fmt.Errorf("acquire migration advisory lock: %w", err)
		}
		log.Warn().
			Int("attempt", attempt).
			Int("max_attempts", advisoryLockAttempts).
			Msg("migration advisory lock held by another process, waiting")
		if err := sleepCtx(ctx, backoffFor(attempt)); err != nil {
			return err
		}
	}
	return fmt.Errorf("acquire migration advisory lock after %d attempts: %w", advisoryLockAttempts, lastErr)
}

// releaseMigrationLock is best-effort, but a connection of unknown lock state
// must never return to the pool still holding the lock.
func releaseMigrationLock(ctx context.Context, conn migrationConn, discard func()) {
	// WithoutCancel: the unlock still runs on a cancelled context, or an aborted
	// boot parks the lock on a pooled session.
	if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockKey); err != nil {
		log.Warn().Err(err).Msg("release migration advisory lock failed")
		if discard != nil {
			discard()
		}
	}
}

// applyMigration retries 55P03 a bounded number of times: a contended table is
// transient (the draining pod commits), so it must not be a fatal boot error.
func applyMigration(ctx context.Context, conn migrationConn, name, version, body string) error {
	var lastErr error
	for attempt := 1; attempt <= migrationLockAttempts; attempt++ {
		err := applyMigrationOnce(ctx, conn, name, version, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isLockNotAvailable(err) {
			return err
		}
		if attempt == migrationLockAttempts {
			break
		}
		log.Warn().
			Err(err).
			Str("migration", name).
			Int("attempt", attempt).
			Int("max_attempts", migrationLockAttempts).
			Msg("migration could not take its table lock, retrying")
		if err := sleepCtx(ctx, backoffFor(attempt)); err != nil {
			return err
		}
	}
	return fmt.Errorf("apply migration %s after %d attempts: %w", name, migrationLockAttempts, lastErr)
}

// applyMigrationOnce records the version in the same transaction, so a crash
// cannot leave the schema and the ledger disagreeing.
func applyMigrationOnce(ctx context.Context, conn migrationConn, name, version, body string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	// Rollback is a no-op after Commit; WithoutCancel so a cancelled context
	// still unwinds the transaction.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// is_local=true (SET LOCAL) reverts at transaction end, so the pinned
	// connection carries no timeout into the next migration or the pool.
	if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true)`, migrationLockTimeout); err != nil {
		return fmt.Errorf("set lock_timeout for %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('statement_timeout', $1, true)`, migrationStatementTimeout); err != nil {
		return fmt.Errorf("set statement_timeout for %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}

// isLockNotAvailable reports whether err is (or wraps) SQLSTATE 55P03.
func isLockNotAvailable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgLockNotAvailable
}

func backoffFor(attempt int) time.Duration {
	delay := retryBaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > 8*time.Second {
			return 8 * time.Second
		}
	}
	return delay
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func ensureSchemaTable(ctx context.Context, conn migrationConn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	return err
}

func loadApplied(ctx context.Context, conn migrationConn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		out[version] = true
	}
	return out, rows.Err()
}

func listMigrationFiles(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".up.sql") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func migrationVersion(name string) string {
	return strings.TrimSuffix(name, ".up.sql")
}

func ListMigrationFiles(fsys fs.FS) ([]string, error) {
	return listMigrationFiles(fsys)
}

func MigrationVersion(name string) string {
	return migrationVersion(name)
}

// DatabaseName parses dsn's database name for the line cmd/migrate prints when
// it is done; any name is accepted, a check in this binary alone would guard
// nothing.
func DatabaseName(dsn string) (string, error) {
	if strings.TrimSpace(dsn) == "" {
		return "", errors.New("empty postgres DSN")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return "", fmt.Errorf("parse postgres DSN: %w", err)
	}
	name := cfg.ConnConfig.Database
	if name == "" {
		return "", errors.New("postgres DSN has no database name")
	}
	return name, nil
}
