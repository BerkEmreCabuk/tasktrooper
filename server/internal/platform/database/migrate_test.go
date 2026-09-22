// An internal test (package database): the guarantees worth testing all live on
// unexported seams; embedded_test.go covers the real-database path externally.
package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/migrations"
)

type MigrateSuite struct {
	suite.Suite
	restoreDelay time.Duration
}

func (s *MigrateSuite) SetupTest() {
	// Collapse the backoff ladder; retry tests assert attempt counts only.
	s.restoreDelay = retryBaseDelay
	retryBaseDelay = time.Millisecond
}

func (s *MigrateSuite) TearDownTest() {
	retryBaseDelay = s.restoreDelay
}

// fakeConn records every statement in order and can inject a failure for a
// chosen one — the only deterministic way to exercise lock contention (55P03).
type fakeConn struct {
	applied []string

	// failExec fails the nth (1-based) Exec whose SQL contains match.
	failExec func(sql string, nth int) error

	stmts []string
	calls map[string]int
}

func newFakeConn(applied ...string) *fakeConn {
	return &fakeConn{applied: applied, calls: map[string]int{}}
}

func (f *fakeConn) record(sql string) error {
	f.stmts = append(f.stmts, sql)
	f.calls[sql]++
	if f.failExec != nil {
		return f.failExec(sql, f.calls[sql])
	}
	return nil
}

func (f *fakeConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.record(sql)
}

func (f *fakeConn) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if err := f.record(sql); err != nil {
		return nil, err
	}
	return &fakeRows{values: f.applied}, nil
}

func (f *fakeConn) Begin(_ context.Context) (pgx.Tx, error) {
	f.stmts = append(f.stmts, "BEGIN")
	return &fakeTx{conn: f}, nil
}

func (f *fakeConn) count(substr string) int {
	n := 0
	for _, s := range f.stmts {
		if strings.Contains(s, substr) {
			n++
		}
	}
	return n
}

func (f *fakeConn) indexOf(substr string) int {
	for i, s := range f.stmts {
		if strings.Contains(s, substr) {
			return i
		}
	}
	return -1
}

type fakeTx struct{ conn *fakeConn }

func (t *fakeTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, t.conn.record(sql)
}
func (t *fakeTx) Commit(context.Context) error {
	t.conn.stmts = append(t.conn.stmts, "COMMIT")
	return nil
}
func (t *fakeTx) Rollback(context.Context) error {
	t.conn.stmts = append(t.conn.stmts, "ROLLBACK")
	return nil
}
func (t *fakeTx) Begin(context.Context) (pgx.Tx, error) {
	return t, nil
}
func (t *fakeTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if err := t.conn.record(sql); err != nil {
		return nil, err
	}
	return &fakeRows{}, nil
}
func (t *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (t *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (t *fakeTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (t *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *fakeTx) Conn() *pgx.Conn { return nil }

type fakeRows struct {
	values []string
	pos    int
}

func (r *fakeRows) Next() bool {
	if r.pos >= len(r.values) {
		return false
	}
	r.pos++
	return true
}
func (r *fakeRows) Scan(dest ...any) error {
	p, ok := dest[0].(*string)
	if !ok {
		return errors.New("unexpected scan target")
	}
	*p = r.values[r.pos-1]
	return nil
}
func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

func lockErr() error  { return &pgconn.PgError{Code: pgLockNotAvailable, Message: "lock timeout"} }
func otherErr() error { return &pgconn.PgError{Code: "42P01", Message: "undefined table"} }

func (s *MigrateSuite) allVersions() []string {
	names, err := ListMigrationFiles(migrations.Up)
	s.Require().NoError(err)
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, MigrationVersion(n))
	}
	return out
}

func (s *MigrateSuite) TestAdvisoryLockTakenAndReleased() {
	conn := newFakeConn(s.allVersions()...)

	s.Require().NoError(runMigrationsOn(context.Background(), conn, nil))

	s.Equal(1, conn.count("pg_advisory_lock"), "lock taken exactly once")
	s.Equal(1, conn.count("pg_advisory_unlock"), "lock released exactly once")
	s.Less(conn.indexOf("pg_advisory_lock"), conn.indexOf("schema_migrations"),
		"the lock must be held before schema_migrations is read, otherwise two pods still race")
	s.Equal("SELECT pg_advisory_unlock($1)", conn.stmts[len(conn.stmts)-1],
		"the unlock must be the last thing the run does")
}

func (s *MigrateSuite) TestAdvisoryLockReleasedOnError() {
	conn := newFakeConn()
	conn.failExec = func(sql string, _ int) error {
		if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS schema_migrations") {
			return otherErr()
		}
		return nil
	}

	err := runMigrationsOn(context.Background(), conn, nil)

	s.Require().Error(err)
	s.Equal(1, conn.count("pg_advisory_lock"))
	s.Equal(1, conn.count("pg_advisory_unlock"), "the lock must be released on the error path too")
}

func (s *MigrateSuite) TestAdvisoryLockNotReleasedWhenNeverAcquired() {
	conn := newFakeConn()
	conn.failExec = func(sql string, _ int) error {
		if strings.Contains(sql, "pg_advisory_lock") {
			return otherErr()
		}
		return nil
	}

	err := runMigrationsOn(context.Background(), conn, nil)

	s.Require().Error(err)
	s.Zero(conn.count("pg_advisory_unlock"),
		"unlocking a lock we never took would be a no-op at best and misleading at worst")
	s.Zero(conn.count("CREATE TABLE IF NOT EXISTS schema_migrations"), "no work without the lock")
}

func (s *MigrateSuite) TestAdvisoryLockWaitsOutPeerPod() {
	conn := newFakeConn(s.allVersions()...)
	conn.failExec = func(sql string, nth int) error {
		if strings.Contains(sql, "pg_advisory_lock") && nth <= 2 {
			return lockErr()
		}
		return nil
	}

	s.Require().NoError(runMigrationsOn(context.Background(), conn, nil))
	s.Equal(3, conn.count("pg_advisory_lock"), "retried twice, acquired on the third attempt")
}

func (s *MigrateSuite) TestAdvisoryLockGivesUpAfterBoundedWait() {
	conn := newFakeConn()
	conn.failExec = func(sql string, _ int) error {
		if strings.Contains(sql, "pg_advisory_lock") {
			return lockErr()
		}
		return nil
	}

	err := runMigrationsOn(context.Background(), conn, nil)

	s.Require().Error(err)
	s.Equal(advisoryLockAttempts, conn.count("pg_advisory_lock"), "bounded, not an infinite hang")
	s.Contains(err.Error(), "advisory lock")
}

func (s *MigrateSuite) TestUnlockFailureDiscardsConnection() {
	conn := newFakeConn(s.allVersions()...)
	conn.failExec = func(sql string, _ int) error {
		if strings.Contains(sql, "pg_advisory_unlock") {
			return otherErr()
		}
		return nil
	}
	discarded := false

	s.Require().NoError(runMigrationsOn(context.Background(), conn, func() { discarded = true }))
	s.True(discarded, "a connection whose lock state is unknown must not go back into the pool")
}

func (s *MigrateSuite) TestTimeoutsSetLocallyBeforeMigrationBody() {
	conn := newFakeConn(s.pendingOnly("001_sessions")...)

	s.Require().NoError(runMigrationsOn(context.Background(), conn, nil))

	lockIdx := conn.indexOf("set_config('lock_timeout', $1, true)")
	stmtIdx := conn.indexOf("set_config('statement_timeout', $1, true)")
	bodyIdx := conn.indexOf("CREATE TABLE IF NOT EXISTS sessions")
	s.Require().NotEqual(-1, lockIdx, "lock_timeout must be set per migration")
	s.Require().NotEqual(-1, stmtIdx, "statement_timeout must be set per migration")
	s.Require().NotEqual(-1, bodyIdx)
	s.Less(lockIdx, bodyIdx, "lock_timeout is useless if it is set after the body took its locks")
	s.Less(stmtIdx, bodyIdx)
	// SET LOCAL, so no timeout leaks into the next migration or the pool.
	s.Contains(conn.stmts[lockIdx], ", true)")
	s.Contains(conn.stmts[stmtIdx], ", true)")
}

func (s *MigrateSuite) TestTimeoutValuesAreSane() {
	lock, err := time.ParseDuration(migrationLockTimeout)
	s.Require().NoError(err)
	stmt, err := time.ParseDuration(strings.Replace(migrationStatementTimeout, "min", "m", 1))
	s.Require().NoError(err)
	advisory, err := time.ParseDuration(advisoryLockTimeout)
	s.Require().NoError(err)

	s.Less(lock, stmt, "a migration that got its locks must not be killed sooner than one waiting for them")
	s.Less(lock, advisory, "waiting on a peer pod's whole run needs more slack than one table lock")
	s.GreaterOrEqual(stmt, time.Minute, "statement_timeout must survive a real backfill")
}

func (s *MigrateSuite) TestMigrationRetriedOnLockNotAvailable() {
	conn := newFakeConn(s.pendingOnly("001_sessions")...)
	conn.failExec = func(sql string, nth int) error {
		if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS sessions") && nth <= 2 {
			return lockErr()
		}
		return nil
	}

	s.Require().NoError(runMigrationsOn(context.Background(), conn, nil))

	s.Equal(3, conn.count("CREATE TABLE IF NOT EXISTS sessions"), "two 55P03s, then success")
	s.Equal(1, conn.count("INSERT INTO schema_migrations"), "recorded exactly once")
	s.GreaterOrEqual(conn.count("ROLLBACK"), 2, "each failed attempt unwinds its transaction")
}

func (s *MigrateSuite) TestMigrationGivesUpAfterBoundedRetries() {
	conn := newFakeConn(s.pendingOnly("001_sessions")...)
	conn.failExec = func(sql string, _ int) error {
		if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS sessions") {
			return lockErr()
		}
		return nil
	}

	err := runMigrationsOn(context.Background(), conn, nil)

	s.Require().Error(err)
	s.Equal(migrationLockAttempts, conn.count("CREATE TABLE IF NOT EXISTS sessions"))
	s.Equal(1, conn.count("pg_advisory_unlock"), "still released")
}

func (s *MigrateSuite) TestNonLockErrorIsNotRetried() {
	conn := newFakeConn(s.pendingOnly("001_sessions")...)
	conn.failExec = func(sql string, _ int) error {
		if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS sessions") {
			return otherErr()
		}
		return nil
	}

	err := runMigrationsOn(context.Background(), conn, nil)

	s.Require().Error(err)
	s.Equal(1, conn.count("CREATE TABLE IF NOT EXISTS sessions"),
		"a broken migration must fail immediately, not five times over")
	s.Contains(err.Error(), "001_sessions")
}

func (s *MigrateSuite) TestIsLockNotAvailableUnwraps() {
	s.True(isLockNotAvailable(fmt.Errorf("apply migration x: %w", lockErr())))
	s.False(isLockNotAvailable(fmt.Errorf("apply migration x: %w", otherErr())))
	s.False(isLockNotAvailable(errors.New("plain")))
}

// pendingOnly returns the "already applied" set, leaving exactly these pending.
func (s *MigrateSuite) pendingOnly(pending ...string) []string {
	skip := map[string]bool{}
	for _, p := range pending {
		skip[p] = true
	}
	var out []string
	for _, v := range s.allVersions() {
		if !skip[v] {
			out = append(out, v)
		}
	}
	return out
}

func (s *MigrateSuite) TestAppliesOnlyPendingInAscendingOrder() {
	pending := []string{"001_sessions", "079_corrective_indexes"}
	conn := newFakeConn(s.pendingOnly(pending...)...)

	s.Require().NoError(runMigrationsOn(context.Background(), conn, nil))

	s.Equal(2, conn.count("INSERT INTO schema_migrations"), "applied versions are not re-applied")
	s.Less(conn.indexOf("CREATE TABLE IF NOT EXISTS sessions"),
		conn.indexOf("idx_sessions_agent_updated"),
		"migrations must run in ascending version order")
}

func (s *MigrateSuite) TestFullyAppliedRunIsANoOp() {
	conn := newFakeConn(s.allVersions()...)

	s.Require().NoError(runMigrationsOn(context.Background(), conn, nil))

	s.Zero(conn.count("INSERT INTO schema_migrations"))
	s.Zero(conn.count("BEGIN"), "nothing pending means no transaction is opened at all")
}

func (s *MigrateSuite) TestEmbeddedMigrationsAreSorted() {
	names, err := ListMigrationFiles(migrations.Up)
	s.Require().NoError(err)
	s.NotEmpty(names)
	s.Equal("001_sessions.up.sql", names[0])
	s.True(sort.StringsAreSorted(names), "embedded migrations must be lexically sorted")
}

func (s *MigrateSuite) TestMigrationVersion() {
	s.Equal("001_sessions", MigrationVersion("001_sessions.up.sql"))
}

func (s *MigrateSuite) TestCorrectiveIndexMigrationIsTransactionSafe() {
	body, err := fs.ReadFile(migrations.Up, "079_corrective_indexes.up.sql")
	s.Require().NoError(err)

	var executable []string
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if i := strings.Index(trimmed, "--"); i >= 0 {
			trimmed = strings.TrimSpace(trimmed[:i])
		}
		executable = append(executable, trimmed)
	}
	sql := strings.Join(executable, "\n")

	// The runner wraps every migration in a transaction; CONCURRENTLY cannot
	// run inside one.
	s.NotContains(sql, "CONCURRENTLY",
		"CREATE INDEX CONCURRENTLY cannot run inside the runner's transaction")

	for _, line := range executable {
		if !strings.HasPrefix(line, "CREATE INDEX") && !strings.HasPrefix(line, "CREATE UNIQUE INDEX") {
			continue
		}
		s.Contains(line, "IF NOT EXISTS",
			"every index must be IF NOT EXISTS so a re-run is a no-op: %s", line)
	}
	s.Equal(9, strings.Count(sql, "CREATE INDEX IF NOT EXISTS"), "all nine audit findings covered")
}

func (s *MigrateSuite) TestDatabaseName() {
	cases := []struct {
		name string
		dsn  string
		want string // "" means the DSN must be rejected
	}{
		{"url", "postgres://u:p@h:5432/tasktrooper?sslmode=disable", "tasktrooper"},
		{"keyword dsn", "host=h user=u password=p dbname=local_llm sslmode=disable", "local_llm"},
		{"any name", "postgres://u:p@h:5432/my_own_db", "my_own_db"},
		{"empty dsn", "", ""},
		{"garbage", "://nope", ""},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			got, err := DatabaseName(tc.dsn)
			if tc.want == "" {
				s.Require().Error(err, "must refuse %q", tc.dsn)
				s.Empty(got)
				return
			}
			s.Require().NoError(err)
			s.Equal(tc.want, got)
		})
	}
}

func TestMigrateSuite(t *testing.T) {
	suite.Run(t, new(MigrateSuite))
}
