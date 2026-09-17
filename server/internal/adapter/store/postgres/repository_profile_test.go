package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// RepositoryProfileStoreSuite pins the upserts behind the project profile.
//
// Migration 119 added sub_project_path to both profile tables' unique keys, but
// the store kept naming the old conflict targets. Postgres rejects such an
// upsert outright (SQLSTATE 42P10), so no section and no proposal was ever
// stored and every repository's profile stayed empty.
type RepositoryProfileStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.RepositoryProfileStore
	repoID uuid.UUID
}

func TestRepositoryProfileStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RepositoryProfileStoreSuite))
}

func (s *RepositoryProfileStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewRepositoryProfileStore(db)

	repo, err := postgres.NewRepositoryStore(db).Create(s.ctx, "profile-test", "", "/tmp/profile-test", "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *RepositoryProfileStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *RepositoryProfileStoreSuite) TestUpsertSectionsTwiceUpdatesInPlace() {
	section := domain.ProfileSection{
		Section:     domain.ProfileSectionStack,
		BodyMD:      "Go 1.24",
		SourcePaths: []string{"go.mod"},
		Origin:      domain.ProfileOriginDerived,
	}
	s.Require().NoError(s.store.UpsertSections(s.ctx, s.repoID, []domain.ProfileSection{section}))

	section.BodyMD = "Go 1.25"
	s.Require().NoError(s.store.UpsertSections(s.ctx, s.repoID, []domain.ProfileSection{section}),
		"the second write goes through ON CONFLICT and must update the stored row")

	got, err := s.store.ListSections(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(got, 1)
	s.Equal("Go 1.25", got[0].BodyMD)
}

func (s *RepositoryProfileStoreSuite) TestReplaceProposalsTwiceUpdatesInPlace() {
	proposal := domain.ProfileProposal{
		Field:  "build_command",
		Value:  json.RawMessage(`"make build"`),
		Label:  "Build command",
		Status: domain.ProposalPending,
	}
	s.Require().NoError(s.store.ReplaceProposals(s.ctx, s.repoID, []domain.ProfileProposal{proposal}))

	proposal.Label = "Build command (make)"
	s.Require().NoError(s.store.ReplaceProposals(s.ctx, s.repoID, []domain.ProfileProposal{proposal}))

	got, err := s.store.ListProposals(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(got, 1)
	s.Equal("Build command (make)", got[0].Label)
}

// TestEveryConflictTargetMatchesAUniqueIndex guards the whole class of bug the
// profile store had. It reads every INSERT ... ON CONFLICT (cols) statement in
// this package's sources and requires a unique index on exactly those columns
// in the fully migrated schema. Postgres only reports a mismatch when the
// statement runs, which for a background writer means a log line nobody sees.
func (s *RepositoryProfileStoreSuite) TestEveryConflictTargetMatchesAUniqueIndex() {
	rows, err := s.pool.Query(s.ctx, `
		SELECT t.relname, array_agg(a.attname::text ORDER BY k.ord)
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE n.nspname = 'public' AND i.indisunique
		GROUP BY t.relname, i.indexrelid`)
	s.Require().NoError(err)
	unique := map[string]bool{}
	for rows.Next() {
		var table string
		var cols []string
		s.Require().NoError(rows.Scan(&table, &cols))
		unique[conflictKey(table, cols)] = true
	}
	rows.Close()
	s.Require().NoError(rows.Err())
	s.Require().NotEmpty(unique)

	files, err := filepath.Glob("*.go")
	s.Require().NoError(err)
	insertRe := regexp.MustCompile(`(?i)INSERT\s+INTO\s+(\w+)`)
	conflictRe := regexp.MustCompile(`(?i)ON\s+CONFLICT\s*\(([^)]*)\)`)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		s.Require().NoError(err)
		text := string(src)
		for _, loc := range conflictRe.FindAllStringSubmatchIndex(text, -1) {
			inserts := insertRe.FindAllStringSubmatch(text[:loc[0]], -1)
			if len(inserts) == 0 {
				continue
			}
			table := inserts[len(inserts)-1][1]
			var cols []string
			for _, c := range strings.Split(text[loc[2]:loc[3]], ",") {
				cols = append(cols, strings.TrimSpace(c))
			}
			checked++
			s.Truef(unique[conflictKey(table, cols)],
				"%s: ON CONFLICT (%s) on %s has no unique index with exactly those columns",
				file, strings.Join(cols, ", "), table)
		}
	}
	s.Greater(checked, 0, "no ON CONFLICT statements found; the scan is broken")
}

func conflictKey(table string, cols []string) string {
	sorted := append([]string(nil), cols...)
	sort.Strings(sorted)
	return table + "|" + strings.Join(sorted, ",")
}
