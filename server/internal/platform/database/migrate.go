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

// migrationAdvisoryLockKey serialises a whole migration run across pods.
//
// Arbitrary but fixed, and deliberately the sibling of schemaAdvisoryLockKey in
// internal/control/store (5212026001): the two guard different databases so
// they cannot actually collide, but keeping them adjacent and distinct means
// both are found by one grep and a future shared database cannot deadlock them.
// Never reuse this value for anything else in this database.
const migrationAdvisoryLockKey int64 = 5212026002

const (
	// migrationLockTimeout bounds how long one migration waits for a table lock.
	//
	// Postgres lock queues are FIFO: a migration that wants ACCESS EXCLUSIVE and
	// queues behind one long-open transaction also blocks every later query on
	// that table for as long as it waits. Without a timeout that wait is
	// unbounded and happens before the HTTP server even starts. Three seconds is
	// long enough to ride out ordinary short OLTP transactions on the draining
	// pod, and short enough that the whole retry ladder below still finishes
	// well inside a pod startup budget.
	migrationLockTimeout = "3s"

	// migrationStatementTimeout bounds how long one migration statement may run
	// once it *has* its locks. Generous on purpose: a migration legitimately
	// rewrites or backfills a table, and a btree build measures ~0.3s per
	// million rows on Postgres 16, so five minutes is roughly a thousandfold
	// headroom over the largest realistic table. Its job is not to be
	// tight, it is to make a runaway migration fail with a clear error instead
	// of pinning locks forever.
	migrationStatementTimeout = "5min"

	// advisoryLockTimeout bounds one attempt at the run-level advisory lock.
	// lock_timeout applies to pg_advisory_lock waits too (verified on Postgres
	// 16), so this is what stops a boot from hanging forever behind a peer pod.
	// Longer than migrationLockTimeout because the holder is another pod working
	// through the whole pending set, not a single statement.
	advisoryLockTimeout = "30s"

	// migrationLockAttempts is how many times a single migration is retried
	// after 55P03. With the backoff below the worst case is ~3s+250ms, 3s+500ms,
	// 3s+1s, 3s+2s, 3s -> about 22s before the pod gives up and restarts.
	migrationLockAttempts = 5

	// advisoryLockAttempts x advisoryLockTimeout is the total budget for waiting
	// on a peer pod: ~5 minutes. Past that something is genuinely wrong and
	// failing loudly beats hanging silently.
	advisoryLockAttempts = 10
)

// retryBaseDelay is the first backoff step; it doubles per attempt, capped at
// 8s. A var rather than a const purely so tests can collapse the ladder instead
// of sleeping through it.
var retryBaseDelay = 250 * time.Millisecond

// pgLockNotAvailable is SQLSTATE 55P03, raised when lock_timeout fires.
const pgLockNotAvailable = "55P03"

// migrationConn is the subset of *pgxpool.Conn the runner needs. Declaring it
// keeps the retry and timeout logic exercisable without a live database.
type migrationConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// RunMigrations applies every pending embedded migration, once, under a
// database-wide advisory lock.
//
// Two processes can migrate one database at once — a server starting while the
// previous one is still shutting down, or cmd/migrate run beside a server.
// Unserialised, the loser of that race failed the INSERT INTO schema_migrations
// with 23505, and cmd/agent-server turns any error here into log.Fatal.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	return runMigrationsPool(ctx, pool, "")
}

// RunMigrationsUpTo is RunMigrations stopping after version (a file name
// without ".up.sql"), so a test can stand a database at an older schema and
// then apply one migration to populated data.
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
	// The lock is session-scoped and pgxpool hands out a different connection
	// per Exec/Query/Begin, so locking "through the pool" would take the lock on
	// one session and then run the DDL on another - guarding nothing. Everything
	// below is therefore pinned to this one acquired connection. That also means
	// a run needs exactly one connection and cannot deadlock a small pool.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	return runMigrationsThrough(ctx, conn, func() {
		// Closing makes Release destroy the connection instead of pooling it.
		// Postgres frees session advisory locks when the backend goes away, so
		// this is the guaranteed escape hatch when the unlock itself failed.
		_ = conn.Conn().Close(context.WithoutCancel(ctx))
	}, upTo)
}

// runMigrationsOn is RunMigrations minus the pool bookkeeping: it holds the
// advisory lock across the whole run on the single pinned connection it is
// given. discard is invoked only when the unlock fails, to get a connection of
// unknown lock state out of circulation.
func runMigrationsOn(ctx context.Context, conn migrationConn, discard func()) error {
	return runMigrationsThrough(ctx, conn, discard, "")
}

// runMigrationsThrough is runMigrationsOn applying nothing past upTo; empty
// means every migration.
func runMigrationsThrough(ctx context.Context, conn migrationConn, discard func(), upTo string) error {
	if err := acquireMigrationLock(ctx, conn); err != nil {
		return err
	}
	// The unlock has to run on every path, including the error path: leaving it
	// held would stall the next pod for as long as this process lives.
	defer releaseMigrationLock(ctx, conn, discard)

	return runMigrationsLocked(ctx, conn, upTo)
}

// runMigrationsLocked is the body of a run; callers must already hold the
// advisory lock on conn.
func runMigrationsLocked(ctx context.Context, conn migrationConn, upTo string) error {
	// ensureSchemaTable sits inside the lock on purpose: CREATE TABLE IF NOT
	// EXISTS is not race-safe in Postgres, concurrent identical DDL can raise
	// 23505 on pg_class_relname_nsp_index.
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

// acquireMigrationLock takes the run-level advisory lock on conn, waiting a
// bounded number of bounded attempts for any peer pod to finish.
func acquireMigrationLock(ctx context.Context, conn migrationConn) error {
	// Session-scoped (is_local=false) so the timeout also covers the
	// pg_advisory_lock call itself, which is the point.
	if _, err := conn.Exec(ctx, `SELECT set_config('lock_timeout', $1, false)`, advisoryLockTimeout); err != nil {
		return fmt.Errorf("set advisory lock_timeout: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= advisoryLockAttempts; attempt++ {
		_, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockKey)
		if err == nil {
			// Hand the connection back to the per-migration SET LOCAL regime
			// with a clean session default.
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

// releaseMigrationLock is best-effort, but a connection whose lock state is
// unknown must never go back into the pool still holding it.
func releaseMigrationLock(ctx context.Context, conn migrationConn, discard func()) {
	// WithoutCancel: the unlock still has to run when the caller's context is
	// already cancelled, otherwise an aborted boot parks the lock on a pooled
	// session and stalls the next pod for as long as this process lives.
	if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockKey); err != nil {
		log.Warn().Err(err).Msg("release migration advisory lock failed")
		if discard != nil {
			discard()
		}
	}
}

// applyMigration applies one migration, retrying a bounded number of times when
// lock_timeout fires (55P03). A contended table is a transient condition - the
// draining pod commits and moves on - so it must not be a fatal boot error.
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

// applyMigrationOnce runs one migration and records its version in the same
// transaction, so a crash can never leave the schema and the ledger disagreeing.
func applyMigrationOnce(ctx context.Context, conn migrationConn, name, version, body string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	// Rollback is a no-op once Commit succeeded; WithoutCancel so a cancelled
	// context still unwinds the transaction rather than leaking it.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// is_local=true is SET LOCAL: both settings revert when this transaction
	// ends, so the pinned connection never carries a timeout into the next
	// migration or back into the pool.
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

// backoffFor returns the delay before attempt+1, doubling per attempt.
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

// DatabaseName parses dsn and returns its database name, for the line
// cmd/migrate prints when it is done.
//
// Any name is accepted: the server applies these same migrations at boot to
// whatever DATABASE_URL names, so a name check in this binary alone would guard
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
