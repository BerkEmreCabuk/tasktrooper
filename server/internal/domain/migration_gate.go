package domain

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// A schema change is the one class of change that cannot be judged from the
// diff alone: it either applies to a live database or it does not. So the
// board detects it from the changed file paths — never from the agent saying
// so — and refuses to promote such a task to production until a stage deploy
// actually ran the migration.

// flywayVersioned matches Flyway/Liquibase style versioned scripts (V1__x.sql,
// V2_1__x.sql, U3__x.sql, R__view.sql).
var flywayVersioned = regexp.MustCompile(`(?i)^[vur][0-9._]*__.+\.sql$`)

// migrationDirs are directory names that only ever hold schema changes.
var migrationDirs = []string{
	"migrations", "migration", "migrate", "db/migrate", "db/migration",
	"prisma/migrations",
	"alembic/versions", "changelogs", "changelog", "liquibase",
}

// DetectMigrationChange returns the changed files that are database schema
// changes. Empty result means the task touches no schema.
func DetectMigrationChange(paths []string) []string {
	var hits []string
	for _, p := range paths {
		if isMigrationPath(p) {
			hits = append(hits, p)
		}
	}
	sort.Strings(hits)
	return hits
}

func isMigrationPath(p string) bool {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return false
	}
	lower := strings.ToLower(p)
	base := path.Base(lower)

	// Vendored or generated trees are not the project's own schema. Compare
	// whole path segments — "somevendor/" must not match, root-level
	// "testdata/…" must.
	for _, skip := range []string{"node_modules", "vendor", "testdata", "site-packages"} {
		if strings.Contains("/"+lower+"/", "/"+skip+"/") {
			return false
		}
	}

	if strings.HasSuffix(base, ".up.sql") || strings.HasSuffix(base, ".down.sql") {
		return true
	}
	if flywayVersioned.MatchString(base) {
		return true
	}
	if strings.HasPrefix(base, "changelog") && (strings.HasSuffix(base, ".xml") ||
		strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") ||
		strings.HasSuffix(base, ".json") || strings.HasSuffix(base, ".sql")) {
		return true
	}
	if base == "schema.prisma" || base == "schema.rb" || base == "structure.sql" {
		return true
	}

	dir := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		dir = lower[:idx]
	} else {
		dir = ""
	}
	for _, md := range migrationDirs {
		// Segment-normalized: matches "migrations", "db/migrations" and
		// "migrations/postgres" alike, at repo root or nested.
		if strings.Contains("/"+dir+"/", "/"+md+"/") {
			// A doc or a test fixture inside a migrations folder is not a schema
			// change; only actual migration artifacts are.
			switch path.Ext(base) {
			case ".sql", ".xml", ".yaml", ".yml", ".json", ".py", ".rb", ".js", ".ts", ".go":
				return true
			}
		}
	}
	return false
}

// Repository test strategies — how a repository's changes are verified before
// they are allowed to move on.
const (
	// TestStrategyLocal runs the tests in the task workspace only; nothing is
	// deployed for QA.
	TestStrategyLocal = "local"
	// TestStrategyStage (default) deploys to staging when a task reaches
	// ready_for_qa, so QA tests a real environment.
	TestStrategyStage = "stage"
	// TestStrategyPerStep deploys to staging at each reviewed step
	// (code_review and ready_for_qa), for repos where every stage of the work
	// has to be exercised on a live environment.
	TestStrategyPerStep = "per_step"
)

func ValidTestStrategy(s string) bool {
	switch s {
	case TestStrategyLocal, TestStrategyStage, TestStrategyPerStep:
		return true
	}
	return false
}

// DeploysForQA reports whether the strategy deploys to staging before QA.
func DeploysForQA(strategy string) bool {
	return strategy != TestStrategyLocal
}

// DeploysOnCodeReview reports whether the strategy also deploys when the task
// enters code review.
func DeploysOnCodeReview(strategy string) bool {
	return strategy == TestStrategyPerStep
}
