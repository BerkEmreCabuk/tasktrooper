package domain_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type MigrationGateSuite struct{ suite.Suite }

func TestMigrationGateSuite(t *testing.T) { suite.Run(t, new(MigrationGateSuite)) }

func (s *MigrationGateSuite) TestDetectsCommonMigrationLayouts() {
	cases := []struct {
		name string
		path string
	}{
		{"golang-migrate up", "apps/backend/migrations/059_add_column.up.sql"},
		{"golang-migrate down", "migrations/059_add_column.down.sql"},
		{"flyway versioned", "src/main/resources/db/migration/V12__add_index.sql"},
		{"flyway repeatable", "db/migration/R__refresh_view.sql"},
		{"liquibase changelog", "src/main/resources/db/changelog-master.xml"},
		{"prisma", "prisma/migrations/20260101_init/migration.sql"},
		{"prisma schema", "prisma/schema.prisma"},
		{"alembic", "alembic/versions/8f3c_add_users.py"},
		{"rails", "db/migrate/20260101120000_create_users.rb"},
		{"rails schema", "db/schema.rb"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal([]string{tc.path}, domain.DetectMigrationChange([]string{tc.path}))
		})
	}
}

func (s *MigrationGateSuite) TestIgnoresNonSchemaChanges() {
	files := []string{
		"internal/application/board/pipeline.go",
		"apps/web/src/api.ts",
		"docs/migrations.md",
		"README.md",
		"internal/store/testdata/migrations/001_seed.up.sql",
		"vendor/github.com/x/y/migrations/001_x.up.sql",
		"node_modules/pkg/prisma/schema.prisma",
	}

	s.Empty(domain.DetectMigrationChange(files))
}

func (s *MigrationGateSuite) TestReturnsEveryHitSorted() {
	hits := domain.DetectMigrationChange([]string{
		"migrations/060_b.up.sql",
		"cmd/main.go",
		"migrations/059_a.up.sql",
	})

	s.Equal([]string{"migrations/059_a.up.sql", "migrations/060_b.up.sql"}, hits)
}

// Windows-style paths reach us from workspaces checked out on Windows agents.
func (s *MigrationGateSuite) TestHandlesBackslashPaths() {
	s.Len(domain.DetectMigrationChange([]string{`apps\backend\migrations\059_x.up.sql`}), 1)
}

func (s *MigrationGateSuite) TestTestStrategyGating() {
	s.True(domain.ValidTestStrategy(domain.TestStrategyLocal))
	s.False(domain.ValidTestStrategy("whenever"))

	s.False(domain.DeploysForQA(domain.TestStrategyLocal), "local strategy must not deploy for QA")
	s.True(domain.DeploysForQA(domain.TestStrategyStage))
	s.True(domain.DeploysForQA(domain.TestStrategyPerStep))

	s.False(domain.DeploysOnCodeReview(domain.TestStrategyStage))
	s.True(domain.DeploysOnCodeReview(domain.TestStrategyPerStep))
}
