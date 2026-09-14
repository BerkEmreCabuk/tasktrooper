package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// DB is the only thing the stores are allowed to hold. It has the same method
// set as *pgxpool.Pool for the four calls this package makes — Query, QueryRow,
// Exec, Begin (plus SendBatch) — so 469 store methods did not have to change
// shape, and it differs in exactly one way: **there is no un-scoped path
// through it.**
//
// Every statement runs inside its own transaction that begins with
//
//	SET LOCAL app.tenant_id = <the caller's tenant>
//
// and every tenant-scoped table has a row-level-security policy reading that
// GUC (migration 114). So a store method that forgets about tenants — which is
// all 469 of them — still cannot see or write another tenant's rows, and a
// caller with no tenant on its context gets tenant.ErrNoTenant instead of a
// result.
//
// SET LOCAL, not SET. The pool is shared by every tenant and a connection goes
// back into it after each statement; a session-level SET would sit on that
// connection and silently become the next tenant's scope. That is not a
// performance detail, it is the difference between isolation and a cross-tenant
// read.
//
// The cost is honest and worth stating: one statement is now BEGIN /
// set_config / statement / COMMIT — four round trips where there was one. It
// buys the property that no future store method can opt out. Where that
// matters, InTx amortises the frame over a whole unit of work.
type DB struct {
	pool *pgxpool.Pool
}

// NewDB wraps a pool. The pool itself is unexported from here on: application
// code reaches the database only through the tenant-scoped surface below.
func NewDB(pool *pgxpool.Pool) *DB { return &DB{pool: pool} }

// Close and Ping are pool lifecycle, not queries, and carry no tenant.
func (d *DB) Close() { d.pool.Close() }

func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// begin opens a transaction already scoped to ctx's tenant.
//
// set_config(..., true) is SET LOCAL with a bound parameter — the tenant is
// never interpolated into SQL text, so a header value can never become a
// statement. It reverts when this transaction ends, which is the whole point.
func (d *DB) begin(ctx context.Context) (pgx.Tx, error) {
	id, ok := tenant.From(ctx)
	if !ok {
		return nil, tenant.ErrNoTenant
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tenant transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, id.TenantID.String()); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("scope transaction to tenant: %w", err)
	}
	return tx, nil
}

// Begin hands a caller a tenant-scoped transaction to drive itself. The
// scoping is already applied, so a multi-statement store method gets it for
// free and pays the frame once.
func (d *DB) Begin(ctx context.Context) (pgx.Tx, error) { return d.begin(ctx) }

// InTx runs fn inside one tenant-scoped transaction, committing on nil and
// rolling back on anything else. Preferred over Begin for new code: it cannot
// leak a transaction on an early return.
func (d *DB) InTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := d.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx, err := d.begin(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return tag, err
	}
	return tag, tx.Commit(ctx)
}

// Query returns rows whose Close() ends the transaction. Callers already
// `defer rows.Close()` — that is the pgx contract — so the transaction is
// closed on exactly the paths the rows are, including an early return out of
// the scan loop.
func (d *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx, err := d.begin(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, err
	}
	return &txRows{Rows: rows, tx: tx, ctx: ctx}, nil
}

// QueryRow's transaction is closed by Scan, the only method pgx.Row has and the
// only thing any caller does with one. A Row that is never scanned would leak
// its transaction until the context dies — pgx has the same hazard and the same
// answer: there is nothing else to do with a Row.
func (d *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx, err := d.begin(ctx)
	if err != nil {
		return errRow{err: err}
	}
	return &txRow{row: tx.QueryRow(ctx, sql, args...), tx: tx, ctx: ctx}
}

// SendBatch keeps pgx's one-round-trip batching, wrapped in the same frame; the
// transaction ends when the caller closes the results.
func (d *DB) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	tx, err := d.begin(ctx)
	if err != nil {
		return errBatchResults{err: err}
	}
	return &txBatchResults{BatchResults: tx.SendBatch(ctx, b), tx: tx, ctx: ctx}
}

// --- the wrappers that close the frame -------------------------------------

type txRows struct {
	pgx.Rows
	tx     pgx.Tx
	ctx    context.Context
	closed bool
}

func (r *txRows) Close() {
	r.Rows.Close()
	if r.closed {
		return
	}
	r.closed = true
	// A read that failed mid-iteration is rolled back rather than committed.
	// Nothing here writes, so the two are equivalent to the database; they are
	// not equivalent in the logs, and "committed a failed statement" is a
	// sentence nobody should have to read while debugging.
	if r.Rows.Err() != nil {
		_ = r.tx.Rollback(context.WithoutCancel(r.ctx))
		return
	}
	_ = r.tx.Commit(context.WithoutCancel(r.ctx))
}

type txRow struct {
	row pgx.Row
	tx  pgx.Tx
	ctx context.Context
}

func (r *txRow) Scan(dest ...any) error {
	if err := r.row.Scan(dest...); err != nil {
		_ = r.tx.Rollback(context.WithoutCancel(r.ctx))
		return err
	}
	// INSERT ... RETURNING goes through here, so the commit is load-bearing:
	// without it every single-statement write in this package would roll back.
	return r.tx.Commit(r.ctx)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type txBatchResults struct {
	pgx.BatchResults
	tx     pgx.Tx
	ctx    context.Context
	closed bool
}

func (b *txBatchResults) Close() error {
	err := b.BatchResults.Close()
	if b.closed {
		return err
	}
	b.closed = true
	if err != nil {
		_ = b.tx.Rollback(context.WithoutCancel(b.ctx))
		return err
	}
	return b.tx.Commit(b.ctx)
}

type errBatchResults struct{ err error }

func (b errBatchResults) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, b.err }
func (b errBatchResults) Query() (pgx.Rows, error)         { return nil, b.err }
func (b errBatchResults) QueryRow() pgx.Row                { return errRow{err: b.err} }
func (b errBatchResults) Close() error                     { return b.err }

// --- the fleet ------------------------------------------------------------

// Tenants lists every tenant this database has served.
//
// This is the ONE query in the package that runs without a tenant scope, and it
// can only be that query: `tenants` is the registry of tenants rather than one
// tenant's data, carries no policy (migration 115) and holds nothing but uuids
// and timestamps. Reading it is what makes EachTenant possible; there is no
// other un-scoped read anywhere in this package, by construction.
func (d *DB) Tenants(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := d.pool.Query(ctx, `SELECT id FROM tenants ORDER BY first_seen_at`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// EnsureTenant records a tenant on first sight and reports whether it still
// needs seeding. It writes the registry, not tenant data, so it too runs
// outside a policy — a tenant cannot be scoped to itself before it exists.
func (d *DB) EnsureTenant(ctx context.Context, id uuid.UUID) (needsBootstrap bool, err error) {
	err = d.pool.QueryRow(ctx, `
		INSERT INTO tenants (id) VALUES ($1)
		ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.id
		RETURNING bootstrapped_at IS NULL
	`, id).Scan(&needsBootstrap)
	if err != nil {
		return false, fmt.Errorf("ensure tenant: %w", err)
	}
	return needsBootstrap, nil
}

// MarkBootstrapped closes the gate EnsureTenant opens.
func (d *DB) MarkBootstrapped(ctx context.Context, id uuid.UUID) error {
	_, err := d.pool.Exec(ctx, `UPDATE tenants SET bootstrapped_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark tenant bootstrapped: %w", err)
	}
	return nil
}

// schemaPool reaches past the tenant scope, and is unexported so that only this
// package can. It exists for the two things in here that are SCHEMA rather than
// tenant data — probing pg_extension and the pgvector column/index DDL — where
// there is no tenant to scope to because the answer is the same for the whole
// database. Anything that touches a row belonging to somebody goes through the
// methods above.
func (d *DB) schemaPool() *pgxpool.Pool { return d.pool }
