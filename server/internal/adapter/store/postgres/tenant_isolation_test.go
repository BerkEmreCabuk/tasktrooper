package postgres_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/tenantboot"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/migrations"
)

// TenantIsolationSuite is the test the whole multi-tenant change stands on: two
// tenants, one table, one pool, and neither able to see the other.
//
// It has to run against a real Postgres because the isolation is not in Go at
// all — it is a row-level-security policy reading a session GUC (migration
// 114). A Go-level fake would only prove that this file's own filter works.
//
// # It reproduces the PRODUCTION role, not a convenient one
//
// deploy/sql/agent-server-role.sql creates `agent_app` as
// `NOSUPERUSER NOBYPASSRLS` and makes it the OWNER of the database, because it
// is the role that runs the migrations. Both halves of that matter and this
// suite reproduces both:
//
//   - A SUPERUSER, and any role with BYPASSRLS, ignores every policy
//     unconditionally. Embedded Postgres hands its bootstrap user exactly those
//     rights, so a suite that used the default connection would pass while
//     asserting nothing.
//   - `ENABLE ROW LEVEL SECURITY` alone exempts the table OWNER. Only
//     `FORCE ROW LEVEL SECURITY` reaches the owner — so a suite that connected
//     as a non-owner would pass even if migration 114 had forgotten every
//     FORCE, which is precisely the mistake that would ship a shared database
//     with no isolation at all.
//
// So: a fresh database named `tasktrooper` (the name production uses, and one
// of the four `tenantDBPattern` accepts), owned by an unprivileged `agent_app`,
// with the migrations applied BY that role from empty. What passes here is what
// production does.
type TenantIsolationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	// admin is the embedded superuser, connected to the same database. It is
	// used for exactly two things: creating the role and reading the catalog.
	// No test asserts isolation through it — it would bypass every policy.
	admin *pgxpool.Pool
	// pool is agent_app: unprivileged, table owner, subject to FORCE RLS.
	pool *pgxpool.Pool
	db   *postgres.DB

	appDSN string

	tenantA uuid.UUID
	tenantB uuid.UUID
}

func TestTenantIsolationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(TenantIsolationSuite))
}

const appRolePassword = "agent_app"

func (s *TenantIsolationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg

	// Bootstrap, as the superuser, exactly what deploy/sql/agent-server-role.sql
	// does by hand at cutover — same attributes, same ownership, same grants.
	boot, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	defer boot.Close()
	for _, stmt := range []string{
		`DROP DATABASE IF EXISTS tasktrooper`,
		`DROP ROLE IF EXISTS agent_app`,
		`CREATE ROLE agent_app LOGIN PASSWORD '` + appRolePassword + `' ` +
			`NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT`,
		`CREATE DATABASE tasktrooper OWNER agent_app`,
		`REVOKE ALL ON DATABASE tasktrooper FROM PUBLIC`,
	} {
		_, err := boot.Exec(s.ctx, stmt)
		s.Require().NoError(err, stmt)
	}

	s.admin, err = pgxpool.New(s.ctx, replaceDatabase(pg.DSN(), "tasktrooper"))
	s.Require().NoError(err)
	// Extensions are installable only by a superuser, which is why the runbook
	// installs them before agent-server's first boot. pgcrypto is mandatory:
	// migration 001 asks for it unguarded. pgvector is not shipped with the
	// embedded binaries; migration 036 wraps it in a DO block and falls back to
	// in-process cosine similarity, so its absence is expected here and is the
	// one production-vs-test difference this suite cannot close.
	for _, stmt := range []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE EXTENSION IF NOT EXISTS pg_trgm`,
		`GRANT USAGE, CREATE ON SCHEMA public TO agent_app`,
		`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
	} {
		_, err := s.admin.Exec(s.ctx, stmt)
		s.Require().NoError(err, stmt)
	}

	s.appDSN = appRoleDSN(pg.DSN())
	s.pool, err = pgxpool.New(s.ctx, s.appDSN)
	s.Require().NoError(err)

	// THE migration run under test: 001 through 116, from empty, as the
	// unprivileged role that owns the database.
	s.Require().NoError(database.RunMigrations(s.ctx, s.pool),
		"the full migration set must apply as a NOSUPERUSER NOBYPASSRLS owner")

	s.db = postgres.NewDB(s.pool)
	s.tenantA = uuid.New()
	s.tenantB = uuid.New()
}

// appRoleDSN swaps the embedded superuser credentials and database for
// agent_app's.
func appRoleDSN(dsn string) string {
	rest := dsn[strings.Index(dsn, "@"):]
	return replaceDatabase("postgres://agent_app:"+appRolePassword+rest, "tasktrooper")
}

// replaceDatabase rewrites the database name in a DSN, keeping the query string.
func replaceDatabase(dsn, db string) string {
	slash := strings.LastIndex(dsn, "/")
	tail := ""
	if q := strings.Index(dsn[slash:], "?"); q >= 0 {
		tail = dsn[slash+q:]
	}
	return dsn[:slash+1] + db + tail
}

func (s *TenantIsolationSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.admin != nil {
		s.admin.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *TenantIsolationSuite) scoped(id uuid.UUID) context.Context {
	return tenant.With(s.ctx, tenant.Identity{TenantID: id, Role: tenant.RoleOwner})
}

// --- 1. the schema is the shape it claims to be ----------------------------

// TestEveryMigrationApplied checks the ledger against the embedded files rather
// than against a remembered number, so adding a migration cannot quietly leave
// this suite asserting an old schema.
func (s *TenantIsolationSuite) TestEveryMigrationApplied() {
	files, err := database.ListMigrationFiles(migrations.Up)
	s.Require().NoError(err)

	var applied int
	s.Require().NoError(s.admin.QueryRow(s.ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&applied))
	s.Equal(len(files), applied, "every embedded migration must be in the ledger")
}

// TestSchemaShape asks the live catalog the questions migration 114's header
// makes claims about, instead of trusting the claims.
//
// The numbers are asserted as relationships, not as constants, wherever a
// relationship says the real thing: "every table with tenant_id has RLS forced"
// survives the 86th table being added; "85" does not.
func (s *TenantIsolationSuite) TestSchemaShape() {
	var total, withTenant, enabled, forced int
	s.Require().NoError(s.admin.QueryRow(s.ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM pg_attribute a
		            WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)),
		       count(*) FILTER (WHERE c.relrowsecurity),
		       count(*) FILTER (WHERE c.relforcerowsecurity)
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'r'
	`).Scan(&total, &withTenant, &enabled, &forced))
	s.T().Logf("tables=%d with tenant_id=%d rls_enabled=%d rls_forced=%d",
		total, withTenant, enabled, forced)

	s.Equal(withTenant, enabled, "every table carrying tenant_id must have RLS ENABLED")
	s.Equal(withTenant, forced, "every table carrying tenant_id must have RLS FORCED")
	s.Equal(2, total-withTenant,
		"only schema_migrations and tenants may be outside the tenant scope")

	// And the two exempt tables are the two that are meant to be.
	s.Equal([]string{"schema_migrations", "tenants"},
		s.strings(`
			SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname='public' AND c.relkind='r'
			   AND NOT EXISTS (SELECT 1 FROM pg_attribute a
			         WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
			 ORDER BY 1`))

	// Enabling RLS without writing a policy denies everything rather than
	// isolating anything, so a policy-less RLS table is its own kind of bug.
	s.Empty(s.strings(`
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		 WHERE n.nspname='public' AND c.relkind='r' AND c.relrowsecurity
		   AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid)`),
		"an RLS table with no policy denies every row instead of isolating them")

	// Every policy must be the same policy: permissive, FOR ALL, and both
	// halves keyed on the GUC. A USING-only policy would leave writes
	// unguarded; a policy on a narrower command would leave the rest open.
	s.Empty(s.strings(`
		SELECT c.relname || '.' || p.polname
		  FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid
		  JOIN pg_namespace n ON n.oid=c.relnamespace
		 WHERE n.nspname='public'
		   AND (p.polcmd <> '*' OR NOT p.polpermissive
		     OR pg_get_expr(p.polqual, c.oid)
		        <> '(tenant_id = (current_setting(''app.tenant_id''::text))::uuid)'
		     OR pg_get_expr(p.polwithcheck, c.oid)
		        <> '(tenant_id = (current_setting(''app.tenant_id''::text))::uuid)')
		 ORDER BY 1`),
		"a policy that is not exactly USING+WITH CHECK on the tenant GUC, FOR ALL")

	// The column itself: NOT NULL, uuid, and defaulted from the GUC. The
	// default is what lets 469 store methods write these tables without
	// naming a tenant; a nullable column would let a row belong to nobody.
	s.Empty(s.strings(`
		SELECT table_name FROM information_schema.columns
		 WHERE table_schema='public' AND column_name='tenant_id'
		   AND (is_nullable <> 'NO' OR data_type <> 'uuid'
		     OR column_default IS DISTINCT FROM '(current_setting(''app.tenant_id''::text))::uuid')
		 ORDER BY 1`),
		"tenant_id must be uuid NOT NULL DEFAULT current_setting('app.tenant_id')::uuid")
}

// TestEveryNaturalUniqueKeyLeadsWithTenant is the constraint half of isolation,
// and the half that fails silently until the SECOND tenant arrives: a UNIQUE
// key that does not lead with tenant_id is unique across the INSTALL, so tenant
// two cannot create an agent called "Developer", a board column called "todo",
// or an MCP server called "github", because tenant one already did.
//
// It enumerates rather than spot-checks, and it deliberately EXEMPTS one shape:
// a single-column uuid key defaulted from gen_random_uuid(). Those are
// surrogate ids, already unique across the fleet by construction, and leaving
// them alone is what keeps the foreign keys pointed at them single-column. The
// exemption is written as "single column AND uuid AND gen_random_uuid()"
// rather than "column named id", because that shortcut is exactly what let
// mcp_servers (id TEXT PRIMARY KEY — the server's NAME) through the first time.
func (s *TenantIsolationSuite) TestEveryNaturalUniqueKeyLeadsWithTenant() {
	offenders := s.strings(`
		SELECT c.relname || ' -> ' || i.relname || ' (' ||
		       (SELECT string_agg(a.attname, ',' ORDER BY k.ord)
		          FROM unnest(idx.indkey::int[]) WITH ORDINALITY AS k(attnum, ord)
		          LEFT JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.attnum) || ')'
		  FROM pg_index idx
		  JOIN pg_class i ON i.oid = idx.indexrelid
		  JOIN pg_class c ON c.oid = idx.indrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND idx.indisunique
		   AND EXISTS (SELECT 1 FROM pg_attribute ta
		         WHERE ta.attrelid=c.oid AND ta.attname='tenant_id' AND NOT ta.attisdropped)
		   AND COALESCE((SELECT a.attname FROM pg_attribute a
		         WHERE a.attrelid=c.oid AND a.attnum=idx.indkey[0]), '') <> 'tenant_id'
		   AND NOT (idx.indnatts = 1 AND EXISTS (
		         SELECT 1 FROM pg_attribute a
		           JOIN information_schema.columns col
		             ON col.table_schema='public' AND col.table_name=c.relname
		            AND col.column_name=a.attname
		          WHERE a.attrelid=c.oid AND a.attnum=idx.indkey[0]
		            AND format_type(a.atttypid, NULL)='uuid'
		            AND col.column_default LIKE 'gen_random_uuid%'))
		 ORDER BY 1`)
	s.Emptyf(offenders,
		"these unique keys are unique across the INSTALL, so the second tenant to "+
			"use the same natural key gets 23505: %v", offenders)
}

// TestEveryForeignKeyCarriesTheTenant is the third way out of a tenant's rows,
// after reads and writes: a REFERENCE.
//
// Postgres enforces a foreign key with an internal query that is deliberately
// exempt from row-level security — it must be, or a policy could hide a parent
// row and silently break the constraint. So a single-column
// `REFERENCES parent(id)` in a shared database is checked against EVERY
// tenant's rows. RLS does not cover foreign keys and cannot be made to.
//
// The only fix is to put tenant_id in the key itself, which is what section 4b
// of migration 114 does. This enumerates the catalog rather than trusting it:
// any FK whose two ends are both tenant-scoped and whose child key does not
// carry tenant_id is a hole.
func (s *TenantIsolationSuite) TestEveryForeignKeyCarriesTheTenant() {
	offenders := s.strings(`
		SELECT c.relname || '.' || con.conname || ' -> ' || f.relname
		  FROM pg_constraint con
		  JOIN pg_class c ON c.oid = con.conrelid
		  JOIN pg_class f ON f.oid = con.confrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE con.contype = 'f' AND n.nspname = 'public'
		   AND EXISTS (SELECT 1 FROM pg_attribute a
		         WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
		   AND EXISTS (SELECT 1 FROM pg_attribute a
		         WHERE a.attrelid=f.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
		   AND NOT EXISTS (SELECT 1 FROM unnest(con.conkey) AS k(attnum)
		         JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.attnum
		        WHERE a.attname='tenant_id')
		 ORDER BY 1`)
	s.Emptyf(offenders,
		"these foreign keys are enforced against every tenant's rows, so one tenant "+
			"can reference another's parent and the parent's owner can then delete or "+
			"null the referencing tenant's data: %v", offenders)

	// And the composite keys really are composite on BOTH sides — a child that
	// carried tenant_id but referenced only the parent's `id` would satisfy the
	// query above while enforcing nothing extra.
	s.Empty(s.strings(`
		SELECT c.relname || '.' || con.conname
		  FROM pg_constraint con
		  JOIN pg_class c ON c.oid = con.conrelid
		  JOIN pg_class f ON f.oid = con.confrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE con.contype = 'f' AND n.nspname = 'public'
		   AND EXISTS (SELECT 1 FROM pg_attribute a
		         WHERE a.attrelid=f.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
		   AND NOT EXISTS (SELECT 1 FROM unnest(con.confkey) AS k(attnum)
		         JOIN pg_attribute a ON a.attrelid=f.oid AND a.attnum=k.attnum
		        WHERE a.attname='tenant_id')
		 ORDER BY 1`),
		"a foreign key carries tenant_id on the child side but does not match it on the parent")
}

// TestCrossTenantReferenceIsRefused is the behaviour the catalog check stands
// for, and the one that was measurably broken before section 4b: tenant B could
// create a board_tasks row pointing at tenant A's repository — a row RLS never
// showed it and never refused — and A deleting that repository then cascaded B's
// task away.
//
// board_tasks.repository_id is chosen because it is nullable and ON DELETE
// CASCADE, i.e. the shape where a composite key is easiest to get wrong: MATCH
// FULL would have rejected every unset repository_id, and a missing key would
// let the cascade cross tenants.
func (s *TenantIsolationSuite) TestCrossTenantReferenceIsRefused() {
	a, b := uuid.New(), uuid.New()

	newRepo := func(id uuid.UUID) uuid.UUID {
		var repo uuid.UUID
		path := "/" + uuid.NewString()
		s.Require().NoError(s.db.QueryRow(s.scoped(id),
			`INSERT INTO repositories (name, root_path) VALUES ($1, $1) RETURNING id`, path).Scan(&repo))
		return repo
	}
	newAgent := func(id uuid.UUID) uuid.UUID {
		var agent uuid.UUID
		s.Require().NoError(s.db.QueryRow(s.scoped(id),
			`INSERT INTO agents (name) VALUES ($1) RETURNING id`, "fk-"+uuid.NewString()[:8]).Scan(&agent))
		return agent
	}
	newTask := func(id, repo uuid.UUID, assignee *uuid.UUID, num int) error {
		_, err := s.db.Exec(s.scoped(id), `
			INSERT INTO board_tasks (title, task_type, task_number, repository_id, assignee_agent_id, board_column)
			VALUES ($1, 'task', $2, $3, $4, 'backlog')`, fmt.Sprintf("t-%d", num), num, repo, assignee)
		return err
	}

	repoA, repoB := newRepo(a), newRepo(b)
	agentA := newAgent(a)

	// The reference tenant B must not be able to make. It KNOWS the uuid —
	// that is the premise, uuids leak into URLs and transcripts — and still
	// cannot use it. repository_id is NOT NULL and ON DELETE CASCADE.
	err := newTask(b, repoA, nil, 1)
	s.Require().Error(err, "tenant B referenced tenant A's repository")
	s.Contains(err.Error(), "foreign key constraint",
		"the refusal must come from the composite key, not from a coincidence")

	// The same for a NULLABLE, ON DELETE SET NULL column, which is the other
	// half of the schema and the half a MATCH FULL key would have broken.
	err = newTask(b, repoB, &agentA, 2)
	s.Require().Error(err, "tenant B referenced tenant A's agent")
	s.Contains(err.Error(), "foreign key constraint")

	// Each tenant's own references still work, and a NULL in the nullable
	// column is still accepted: with MATCH SIMPLE a composite key with any
	// NULL member is not checked. MATCH FULL would reject this row.
	s.Require().NoError(newTask(a, repoA, &agentA, 1), "tenant A's own reference")
	s.Require().NoError(newTask(b, repoB, nil, 3), "a NULL assignee must still be allowed")

	// And a tenant deleting its own parent reaches only its own children.
	tag, err := s.db.Exec(s.scoped(a), `DELETE`+` FROM repositories WHERE id = $1`, repoA)
	s.Require().NoError(err)
	s.Require().EqualValues(1, tag.RowsAffected())

	var aTasks, bTasks int
	s.Require().NoError(s.db.QueryRow(s.scoped(a), `SELECT count(*) FROM board_tasks`).Scan(&aTasks))
	s.Require().NoError(s.db.QueryRow(s.scoped(b), `SELECT count(*) FROM board_tasks`).Scan(&bTasks))
	s.Equal(0, aTasks, "tenant A's own cascade did not run")
	s.Equal(1, bTasks, "tenant A's delete destroyed tenant B's rows")
}

// TestNaturalKeysRepeatAcrossTenants is the behavioural twin of the catalog
// check above: the same natural key, written by two tenants, has to succeed.
//
// mcp_servers is listed first because it is the one this suite caught. Its PK
// is `id TEXT` — the server's caller-supplied name ("github", "playwright") —
// and migration 114's generator skipped it along with the uuid surrogates
// because the column happened to be called `id`.
func (s *TenantIsolationSuite) TestNaturalKeysRepeatAcrossTenants() {
	a, b := uuid.New(), uuid.New()
	shared := "shared-" + uuid.NewString()[:8]

	for _, probe := range tenantTableProbes(shared) {
		for _, id := range []uuid.UUID{a, b} {
			_, err := s.db.Exec(s.scoped(id), probe.insert, probe.args...)
			s.Require().NoErrorf(err,
				"%s: both tenants must be able to use the natural key %q", probe.table, shared)
		}
	}
}

// --- 2. isolation, as the unprivileged owner -------------------------------

// tableProbe is one table, one insert that names no tenant (the column DEFAULT
// supplies it), and the label that makes the row identifiable.
type tableProbe struct {
	table  string
	insert string
	args   []any
}

// tenantTableProbes is the representative spread: a natural-key PRIMARY KEY
// (app_settings, board_task_counters, model_prices, mcp_servers), a uuid
// surrogate with a natural UNIQUE beside it (agents, board_columns, api_keys),
// a composite-key mirror table (tenant_members), and the two that were re-cut
// by hand (board_settings/billing_plan are singletons and are covered by the
// tenantboot suite instead). One table would prove one policy; these prove the
// pattern held across the shapes the schema actually has.
func tenantTableProbes(label string) []tableProbe {
	return []tableProbe{
		{"mcp_servers", `INSERT INTO mcp_servers (id) VALUES ($1)`, []any{label}},
		{"agents", `INSERT INTO agents (name) VALUES ($1)`, []any{label}},
		{"board_columns", `INSERT INTO board_columns (slug, label) VALUES ($1, $1)`, []any{label}},
		{"app_settings", `INSERT INTO app_settings (key, value) VALUES ($1, 'v')`, []any{label}},
		{"board_task_counters", `INSERT INTO board_task_counters (task_type) VALUES ($1)`, []any{label}},
		{"model_prices", `INSERT INTO model_prices (model) VALUES ($1)`, []any{label}},
		{"api_keys", `INSERT INTO api_keys (name, key_hash, key_prefix) VALUES ($1, $1, 'tt')`, []any{label}},
		{"tenant_members", `INSERT INTO tenant_members (user_id) VALUES ($1)`, []any{label}},
	}
}

// TestIsolationAcrossATableSpread is the headline, widened: two tenants write
// the SAME natural key into eight tables of four different key shapes, and
// each sees exactly one row — its own.
//
// Two tenants used only by this test, so "exactly one row" is an exact
// assertion rather than a delta: a leak shows up as 2.
func (s *TenantIsolationSuite) TestIsolationAcrossATableSpread() {
	a, b := uuid.New(), uuid.New()
	label := "spread-" + uuid.NewString()[:8]

	for _, probe := range tenantTableProbes(label) {
		for _, id := range []uuid.UUID{a, b} {
			_, err := s.db.Exec(s.scoped(id), probe.insert, probe.args...)
			s.Require().NoErrorf(err, "insert into %s", probe.table)
		}
	}

	for _, probe := range tenantTableProbes(label) {
		for _, id := range []uuid.UUID{a, b} {
			var visible, mine int
			err := s.db.QueryRow(s.scoped(id),
				fmt.Sprintf(`SELECT count(*), count(*) FILTER (WHERE tenant_id = $1) FROM %s`, probe.table),
				id,
			).Scan(&visible, &mine)
			s.Require().NoErrorf(err, "read %s", probe.table)
			s.Equalf(1, visible, "%s: tenant %s must see exactly its own row, saw %d", probe.table, id, visible)
			s.Equalf(1, mine, "%s: the visible row belongs to another tenant", probe.table)
		}
	}
}

// TestEachTenantSeesOnlyItsOwnRows runs the same property through a real store
// rather than raw SQL, so the wrapper, the store and the policy are all in the
// path — which is the arrangement production actually uses.
func (s *TenantIsolationSuite) TestEachTenantSeesOnlyItsOwnRows() {
	agents := postgres.NewCatalogStore(s.db)

	a, err := agents.CreateAgent(s.scoped(s.tenantA), domain.Agent{
		Name: "Developer", SystemPrompt: "tenant A", Enabled: true,
	})
	s.Require().NoError(err)
	b, err := agents.CreateAgent(s.scoped(s.tenantB), domain.Agent{
		Name: "Developer", SystemPrompt: "tenant B", Enabled: true,
	})
	s.Require().NoError(err)
	s.Require().NotEqual(a.ID, b.ID)

	listA, err := agents.ListAgents(s.scoped(s.tenantA))
	s.Require().NoError(err)
	s.Require().Len(listA, 1, "tenant A must see exactly its own agent")
	s.Equal("tenant A", listA[0].SystemPrompt)

	listB, err := agents.ListAgents(s.scoped(s.tenantB))
	s.Require().NoError(err)
	s.Require().Len(listB, 1, "tenant B must see exactly its own agent")
	s.Equal("tenant B", listB[0].SystemPrompt)
}

// TestOneTenantCannotReadAnotherByID closes the obvious hole in the test above:
// listing is filtered, but so must a lookup by a primary key that was leaked or
// guessed be. Under RLS the row simply is not there.
func (s *TenantIsolationSuite) TestOneTenantCannotReadAnotherByID() {
	agents := postgres.NewCatalogStore(s.db)
	owned, err := agents.CreateAgent(s.scoped(s.tenantA), domain.Agent{
		Name: "Secret-" + uuid.NewString(), SystemPrompt: "A only", Enabled: true,
	})
	s.Require().NoError(err)

	_, err = agents.GetAgent(s.scoped(s.tenantB), owned.ID)
	s.Require().Error(err, "tenant B must not be able to fetch tenant A's agent by id")
}

// TestNoTenantIsAnError is the fail-closed half. A query issued with no tenant
// on the context must not return everything, and must not return nothing
// either — silence would be indistinguishable from an empty board. It errors.
//
// Both layers are checked, because either alone would be a false sense of
// safety: postgres.DB refuses before it opens a transaction, and the database
// itself refuses if a statement ever reaches it without the GUC. The second
// half is only meaningful because this pool is agent_app: as the table owner it
// is reached by the policy solely because migration 114 said FORCE.
func (s *TenantIsolationSuite) TestNoTenantIsAnError() {
	agents := postgres.NewCatalogStore(s.db)

	_, err := agents.ListAgents(s.ctx)
	s.Require().ErrorIs(err, tenant.ErrNoTenant, "an un-scoped read must fail, not fall back to a tenant")

	// And with the Go guard bypassed entirely: raw pool, no GUC, straight at
	// the policy.
	var n int
	err = s.pool.QueryRow(s.ctx, `SELECT count(*) FROM agents`).Scan(&n)
	s.Require().Error(err, "the policy itself must refuse a statement with no app.tenant_id")
	s.assertUnscopedRefusal(err)
}

// TestForceRowLevelSecurityReachesTheOwner states the property the rest of the
// suite silently depends on, so that if it ever stops holding the failure names
// the cause instead of appearing as eight unrelated leaks.
func (s *TenantIsolationSuite) TestForceRowLevelSecurityReachesTheOwner() {
	var owner string
	s.Require().NoError(s.admin.QueryRow(s.ctx,
		`SELECT pg_get_userbyid(relowner) FROM pg_class WHERE relname='agents'`).Scan(&owner))
	s.Equal("agent_app", owner, "the suite must connect as the table OWNER, or FORCE is untested")

	var super, bypass bool
	s.Require().NoError(s.admin.QueryRow(s.ctx,
		`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname='agent_app'`).Scan(&super, &bypass))
	s.False(super, "a SUPERUSER ignores every policy unconditionally")
	s.False(bypass, "BYPASSRLS ignores every policy unconditionally")

	// Attributes are not inherited through membership, but a member may
	// SET ROLE and run as the stronger role from then on — so the reachable
	// set matters as much as the role's own flags. This is verification 4(b)
	// of deploy/sql/agent-server-role.sql, run as a test.
	s.Empty(s.strings(`
		SELECT r.rolname FROM pg_roles r
		 WHERE r.rolname <> 'agent_app'
		   AND pg_has_role('agent_app', r.oid, 'MEMBER')
		   AND (r.rolsuper OR r.rolbypassrls)`),
		"agent_app can SET ROLE to a role that bypasses RLS")
}

// assertUnscopedRefusal accepts either of the two ways Postgres refuses an
// un-scoped statement, because which one a connection gives depends on whether
// it has ever been scoped.
//
//   - A connection that has never seen the parameter raises
//     `unrecognized configuration parameter "app.tenant_id"`.
//   - A connection that has served a tenant reverts SET LOCAL to the
//     parameter's session value, which is the EMPTY STRING - so the policy's
//     `current_setting('app.tenant_id')::uuid` fails the cast instead:
//     `invalid input syntax for type uuid: ""`.
//
// Both are hard errors, which is the property that matters: neither degrades
// into "no rows" (indistinguishable from an empty board) or "all rows" (the
// leak). This helper exists so that reading the assertion explains the second
// case rather than leaving the next person to rediscover it.
func (s *TenantIsolationSuite) assertUnscopedRefusal(err error) {
	s.T().Helper()
	msg := err.Error()
	s.Truef(
		strings.Contains(msg, "app.tenant_id") ||
			strings.Contains(msg, `invalid input syntax for type uuid: ""`),
		"want a refusal naming app.tenant_id or a failed empty-uuid cast, got %q", msg)
}

// TestGUCDoesNotLeakBetweenPooledConnections is why the scoping is SET LOCAL
// inside a transaction and not a plain SET.
//
// The pool hands the same physical connection back out; a session-level SET
// would still be on it when the next tenant borrowed it. Running many
// alternating statements makes a connection reuse overwhelmingly likely, and
// every one of them still has to see only its own row.
func (s *TenantIsolationSuite) TestGUCDoesNotLeakBetweenPooledConnections() {
	agents := postgres.NewCatalogStore(s.db)
	for i := 0; i < 20; i++ {
		for _, id := range []uuid.UUID{s.tenantA, s.tenantB} {
			list, err := agents.ListAgents(s.scoped(id))
			s.Require().NoError(err)
			for _, a := range list {
				s.Require().NotEmpty(a.Name)
			}
		}
	}
	// A statement issued with no tenant AFTER all of that must still fail: if a
	// previous transaction had leaked its GUC onto the connection, this would
	// quietly succeed and report somebody's agents.
	var n int
	err := s.pool.QueryRow(s.ctx, `SELECT count(*) FROM agents`).Scan(&n)
	s.Require().Error(err, "app.tenant_id leaked out of a transaction onto a pooled connection")
	s.assertUnscopedRefusal(err)
}

// TestConcurrentTenantsShareConnectionsWithoutSharingRows is the leak test with
// the one condition that makes it mean anything: MORE TENANTS THAN
// CONNECTIONS, all in flight at once.
//
// A sequential loop can pass on a pool that never actually recycles a
// connection mid-flight. Here the pool is capped at two connections and six
// tenants contend for them, so every tenant is repeatedly handed a connection
// that another tenant's transaction just finished with. If the scoping were
// `SET` rather than `SET LOCAL` — or if any path committed without the frame —
// a reader would see the previous borrower's rows, and the exact-count
// assertion below turns that into a failure rather than a shrug.
func (s *TenantIsolationSuite) TestConcurrentTenantsShareConnectionsWithoutSharingRows() {
	const (
		tenants  = 6
		conns    = 2
		rounds   = 25
		perRound = 1
	)

	small, err := pgxpool.New(s.ctx, s.appDSN+"&pool_max_conns="+fmt.Sprint(conns))
	s.Require().NoError(err)
	defer small.Close()
	// Assert the cap took. A DSN parameter that were silently ignored would
	// give every tenant its own connection and this test would prove nothing.
	s.Require().EqualValues(conns, small.Config().MaxConns)
	db := postgres.NewDB(small)

	ids := make([]uuid.UUID, tenants)
	for i := range ids {
		ids[i] = uuid.New()
	}

	g, gctx := errgroup.WithContext(s.ctx)
	for i, id := range ids {
		i, id := i, id
		g.Go(func() error {
			ctx := tenant.With(gctx, tenant.Identity{TenantID: id, Role: tenant.RoleOwner})
			for r := 0; r < rounds; r++ {
				name := fmt.Sprintf("concurrent-%d-%d", i, r)
				if _, err := db.Exec(ctx, `INSERT INTO agents (name) VALUES ($1)`, name); err != nil {
					return fmt.Errorf("tenant %d insert %d: %w", i, r, err)
				}
				// Two reads per round: a count, which a leak inflates, and a
				// tenant_id check, which a leak makes non-uniform.
				var n int
				var distinct int
				if err := db.QueryRow(ctx,
					`SELECT count(*), count(DISTINCT tenant_id) FROM agents`).Scan(&n, &distinct); err != nil {
					return fmt.Errorf("tenant %d read %d: %w", i, r, err)
				}
				if n != (r+1)*perRound {
					return fmt.Errorf("tenant %d round %d: saw %d agents, want %d — another tenant's rows are visible",
						i, r, n, (r+1)*perRound)
				}
				if distinct != 1 {
					return fmt.Errorf("tenant %d round %d: rows from %d tenants visible", i, r, distinct)
				}
				// The probe that makes this a SET LOCAL test rather than a
				// "does the wrapper set the GUC" test: a statement on the SAME
				// small pool, issued with no scope at all, while five other
				// tenants are mid-flight on those two connections. It has to be
				// refused. If the wrapper used a session-level SET the value
				// would still be sitting on whichever connection this borrows
				// and the count would come back — some other tenant's count.
				var leaked int
				if err := small.QueryRow(gctx, `SELECT count(*) FROM agents`).Scan(&leaked); err == nil {
					return fmt.Errorf(
						"an unscoped statement on a shared connection returned %d rows: "+
							"app.tenant_id outlived its transaction", leaked)
				}
			}
			return nil
		})
	}
	s.Require().NoError(g.Wait())

	// Every tenant's writes really landed, and landed under the right tenant.
	for i, id := range ids {
		var n int
		s.Require().NoError(s.db.QueryRow(s.scoped(id),
			`SELECT count(*) FROM agents WHERE name LIKE $1`,
			fmt.Sprintf("concurrent-%d-%%", i)).Scan(&n))
		s.Equal(rounds, n, "tenant %d lost writes", i)
	}
}

// --- 3. the WITH CHECK half: writes ----------------------------------------

// TestInsertCannotPlantARowUnderAnotherTenant is the attack the column DEFAULT
// does not stop on its own. The default only supplies tenant_id when the
// statement omits it; a statement that NAMES tenant_id overrides the default
// entirely, so the only thing standing between a crafted INSERT and a row in
// somebody else's account is the policy's WITH CHECK half.
func (s *TenantIsolationSuite) TestInsertCannotPlantARowUnderAnotherTenant() {
	victim := uuid.New()
	attacker := uuid.New()
	name := "planted-" + uuid.NewString()[:8]

	_, err := s.db.Exec(s.scoped(attacker),
		`INSERT INTO agents (name, tenant_id) VALUES ($1, $2)`, name, victim)
	s.Require().Error(err, "an INSERT naming another tenant's id must be refused")
	s.Contains(err.Error(), "row-level security",
		"the refusal must come from the policy, not from a coincidence")

	var n int
	s.Require().NoError(s.db.QueryRow(s.scoped(victim),
		`SELECT count(*) FROM agents WHERE name = $1`, name).Scan(&n))
	s.Zero(n, "a row was planted in another tenant's account")
}

// TestCrossTenantUpdateAndDeleteAffectZeroRows is the quiet failure mode. An
// UPDATE or DELETE that matches no visible row does not error — it reports zero
// rows and the caller moves on. That is the correct behaviour and it is worth
// asserting explicitly, because the alternative (a policy with USING on SELECT
// only) would report ONE row and the caller would move on just as happily,
// having modified somebody else's data.
func (s *TenantIsolationSuite) TestCrossTenantUpdateAndDeleteAffectZeroRows() {
	agents := postgres.NewCatalogStore(s.db)
	victim := uuid.New()
	attacker := uuid.New()

	target, err := agents.CreateAgent(s.scoped(victim), domain.Agent{
		Name: "Victim-" + uuid.NewString()[:8], SystemPrompt: "original", Enabled: true,
	})
	s.Require().NoError(err)

	tag, err := s.db.Exec(s.scoped(attacker),
		`UPDATE agents SET system_prompt = 'owned' WHERE id = $1`, target.ID)
	s.Require().NoError(err, "the UPDATE matches nothing; it is not an error")
	s.EqualValues(0, tag.RowsAffected(), "a cross-tenant UPDATE modified rows")

	tag, err = s.db.Exec(s.scoped(attacker), `DELETE FROM agents WHERE id = $1`, target.ID)
	s.Require().NoError(err)
	s.EqualValues(0, tag.RowsAffected(), "a cross-tenant DELETE removed rows")

	// Untouched, and still the victim's.
	still, err := agents.GetAgent(s.scoped(victim), target.ID)
	s.Require().NoError(err, "the victim's row was deleted by another tenant")
	s.Equal("original", still.SystemPrompt, "the victim's row was modified by another tenant")
}

// TestWritesCannotCrossTenants covers the remaining WITH CHECK case: not
// planting a NEW row elsewhere, but handing an EXISTING one over. USING alone
// would allow it, and it is a write leak rather than a read leak — just as bad.
func (s *TenantIsolationSuite) TestWritesCannotCrossTenants() {
	agents := postgres.NewCatalogStore(s.db)
	mine, err := agents.CreateAgent(s.scoped(s.tenantA), domain.Agent{
		Name: "Movable-" + uuid.NewString(), SystemPrompt: "A", Enabled: true,
	})
	s.Require().NoError(err)

	tag, err := s.db.Exec(s.scoped(s.tenantA),
		`UPDATE agents SET tenant_id = $1 WHERE id = $2`, s.tenantB, mine.ID)
	if err == nil {
		s.Require().EqualValues(0, tag.RowsAffected(),
			"a tenant must not be able to hand its row to another tenant")
	}

	// Whatever the database chose to call it, the row is still tenant A's.
	still, err := agents.GetAgent(s.scoped(s.tenantA), mine.ID)
	s.Require().NoError(err)
	s.Equal(mine.ID, still.ID)
}

// --- 4. a new tenant gets a working workspace ------------------------------

// TestTenantBootSeedsAWorkingBoard runs the real seeding path — the one that
// replaced the per-tenant INSERTs migrations used to do — and then asks whether
// what a tenant needs to function is actually there.
//
// It checks counts rather than existence: "board_columns is non-empty" would
// pass on a half-applied seed, and a board missing `code_review` is a board
// whose agents silently never get work.
func (s *TenantIsolationSuite) TestTenantBootSeedsAWorkingBoard() {
	boot := tenantboot.NewService(s.db, postgres.NewTenantSeedStore(s.db))

	fresh := uuid.New()
	ctx := tenant.With(s.ctx, tenant.Identity{
		TenantID: fresh, Role: tenant.RoleOwner, UserID: "uid-" + uuid.NewString()[:8],
	})
	s.Require().NoError(boot.Sight(ctx, mustIdentity(ctx)))

	s.Equal(13, s.countFor(fresh, "board_columns"), "the 13-column default board")
	s.Equal(3, s.countFor(fresh, "board_task_counters"), "one counter per task type")
	s.Equal(1, s.countFor(fresh, "board_settings"), "the single-row board settings")
	s.Equal(1, s.countFor(fresh, "billing_plan"), "the single-row billing plan")
	s.Positive(s.countFor(fresh, "app_settings"), "the settings a tenant needs before configuring anything")
	s.Positive(s.countFor(fresh, "llm_provider_configs"), "the provider catalog")
	s.Positive(s.countFor(fresh, "model_prices"), "the default price sheet")

	// The slugs are the part that must not drift: board_tasks.board_column is
	// validated against them and agent subscriptions name them.
	slugs := s.stringsFor(fresh, `SELECT slug FROM board_columns ORDER BY position`)
	s.Equal([]string{
		"backlog", "todo", "in_progress", "analiz_review", "code_review",
		"ready_for_qa", "in_qa", "need_revision", "pm_uat", "human_uat",
		"blocked", "done", "released",
	}, slugs)
}

// TestTenantBootIsIdempotent covers the two ways it gets run twice: the same
// process (the `seeded` map short-circuits) and a cold one (the map is empty
// and the `tenants.bootstrapped_at` gate is what stops the re-seed). The second
// is the one that matters, because a pod restart is not a rare event.
func (s *TenantIsolationSuite) TestTenantBootIsIdempotent() {
	seeds := postgres.NewTenantSeedStore(s.db)
	fresh := uuid.New()
	ctx := tenant.With(s.ctx, tenant.Identity{
		TenantID: fresh, Role: tenant.RoleOwner, UserID: "repeat-uid",
	})

	first := tenantboot.NewService(s.db, seeds)
	s.Require().NoError(first.Sight(ctx, mustIdentity(ctx)))
	before := s.countFor(fresh, "board_columns")

	// Same process, warm cache.
	s.Require().NoError(first.Sight(ctx, mustIdentity(ctx)))
	// A restarted process: a brand-new Service with an empty `seeded` map, so
	// the gate has to come from the database.
	cold := tenantboot.NewService(s.db, seeds)
	s.Require().NoError(cold.Sight(ctx, mustIdentity(ctx)), "re-seeding a known tenant must not fail")

	s.Equal(before, s.countFor(fresh, "board_columns"), "re-running the seed duplicated the board")
	s.Equal(1, s.countFor(fresh, "board_settings"), "re-running the seed duplicated a singleton")
}

// TestSecondTenantGetsItsOwnBoard is the tenant-two case in the form it would
// actually arrive: not a crafted INSERT, but the second person to sign up.
//
// Every row the seed writes uses a natural key the first tenant already used —
// slug 'backlog', task_type 'task', key 'workspace_root', provider_type
// 'local', model 'claude-opus-5', id = 1. Under the pre-114 constraints this is
// thirteen-plus 23505s and a tenant that never gets a board.
func (s *TenantIsolationSuite) TestSecondTenantGetsItsOwnBoard() {
	boot := tenantboot.NewService(s.db, postgres.NewTenantSeedStore(s.db))

	first, second := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{first, second} {
		ctx := tenant.With(s.ctx, tenant.Identity{
			TenantID: id, Role: tenant.RoleOwner, UserID: "uid-" + id.String()[:8],
		})
		s.Require().NoErrorf(boot.Sight(ctx, mustIdentity(ctx)),
			"tenant %s could not be seeded — a global UNIQUE key survived migration 114", id)
	}

	for _, id := range []uuid.UUID{first, second} {
		s.Equal(13, s.countFor(id, "board_columns"), "tenant %s has its own columns", id)
		s.Equal(1, s.countFor(id, "board_settings"), "tenant %s has its own settings row", id)
	}

	// And the two boards are genuinely separate rows, not one board seen twice.
	var distinct int
	s.Require().NoError(s.admin.QueryRow(s.ctx,
		`SELECT count(DISTINCT tenant_id) FROM board_columns WHERE tenant_id = ANY($1)`,
		[]uuid.UUID{first, second}).Scan(&distinct))
	s.Equal(2, distinct)
}

// --- helpers ---------------------------------------------------------------

// mustIdentity re-reads the identity a test just put on a context, so the test
// states it once.
func mustIdentity(ctx context.Context) tenant.Identity {
	id, _ := tenant.From(ctx)
	return id
}

// strings runs a catalog query on the ADMIN connection and returns one column.
// Catalog reads go through the superuser deliberately: pg_class and pg_policy
// are not tenant data and agent_app's view of them is narrower.
func (s *TenantIsolationSuite) strings(sql string, args ...any) []string {
	s.T().Helper()
	rows, err := s.admin.Query(s.ctx, sql, args...)
	s.Require().NoError(err)
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		s.Require().NoError(rows.Scan(&v))
		out = append(out, v)
	}
	s.Require().NoError(rows.Err())
	return out
}

// countFor counts a table AS the tenant, through the scoped wrapper — so the
// count is what that tenant can see, which is the thing under test.
func (s *TenantIsolationSuite) countFor(id uuid.UUID, table string) int {
	s.T().Helper()
	var n int
	s.Require().NoError(s.db.QueryRow(s.scoped(id),
		fmt.Sprintf(`SELECT count(*) FROM %s`, table)).Scan(&n))
	return n
}

func (s *TenantIsolationSuite) stringsFor(id uuid.UUID, sql string) []string {
	s.T().Helper()
	rows, err := s.db.Query(s.scoped(id), sql)
	s.Require().NoError(err)
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		s.Require().NoError(rows.Scan(&v))
		out = append(out, v)
	}
	s.Require().NoError(rows.Err())
	return out
}
