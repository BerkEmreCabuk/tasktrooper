package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func intPtr(n int) *int { return &n }

func TestSettingsConcurrencyLimitsRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pg, err := newTestDatabase(ctx)
	if err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	defer pg.Stop()

	pool, err := pgxpool.New(ctx, pg.DSN())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	store := postgres.NewSettingsStore(postgres.NewDB(pool), "./data/workspaces", "en", false)

	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("get defaults: %v", err)
	}
	if got.MaxConcurrentAgents != 0 || got.MaxConcurrentTasks != 0 {
		t.Fatalf("fresh install must default to unlimited (0), got agents=%d tasks=%d", got.MaxConcurrentAgents, got.MaxConcurrentTasks)
	}

	if _, err := store.Update(ctx, domain.UpdateSettingsRequest{MaxConcurrentAgents: intPtr(2), MaxConcurrentTasks: intPtr(1)}); err != nil {
		t.Fatalf("update limits: %v", err)
	}
	got, err = store.Get(ctx)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.MaxConcurrentAgents != 2 || got.MaxConcurrentTasks != 1 {
		t.Fatalf("limits not persisted, got agents=%d tasks=%d", got.MaxConcurrentAgents, got.MaxConcurrentTasks)
	}

	if _, err := store.Update(ctx, domain.UpdateSettingsRequest{MaxConcurrentAgents: intPtr(0)}); err != nil {
		t.Fatalf("reset agent cap to unlimited: %v", err)
	}
	got, err = store.Get(ctx)
	if err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	if got.MaxConcurrentAgents != 0 || got.MaxConcurrentTasks != 1 {
		t.Fatalf("agent reset must not touch tasks, got agents=%d tasks=%d", got.MaxConcurrentAgents, got.MaxConcurrentTasks)
	}

	if _, err := store.Update(ctx, domain.UpdateSettingsRequest{MaxConcurrentAgents: intPtr(-1)}); err == nil {
		t.Fatal("negative limit must be rejected")
	}

	// A request that mentions only one side must leave the other untouched.
	if _, err := store.Update(ctx, domain.UpdateSettingsRequest{MaxConcurrentAgents: intPtr(3)}); err != nil {
		t.Fatalf("partial update: %v", err)
	}
	got, err = store.Get(ctx)
	if err != nil {
		t.Fatalf("get after partial update: %v", err)
	}
	if got.MaxConcurrentAgents != 3 || got.MaxConcurrentTasks != 1 {
		t.Fatalf("untouched key was rewritten, got agents=%d tasks=%d", got.MaxConcurrentAgents, got.MaxConcurrentTasks)
	}
}

// Garbage or absent values in the table read as 0 (unlimited) rather than
// breaking every settings read.
func TestSettingsConcurrencyLimitsTolerateGarbage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pg, err := newTestDatabase(ctx)
	if err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	defer pg.Stop()

	pool, err := pgxpool.New(ctx, pg.DSN())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	db := postgres.NewDB(pool)
	store := postgres.NewSettingsStore(db, "./data/workspaces", "en", false)

	for _, key := range []string{"max_concurrent_agents", "max_concurrent_tasks"} {
		if _, err := db.Exec(ctx,
			`INSERT INTO app_settings (key, value, updated_at) VALUES ($1, 'oops', now()) ON CONFLICT (key) DO NOTHING`, key); err != nil {
			t.Fatalf("seed garbage for %s: %v", key, err)
		}
	}

	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("get with garbage: %v", err)
	}
	if got.MaxConcurrentAgents != 0 || got.MaxConcurrentTasks != 0 {
		t.Fatalf("garbage must read as unlimited, got agents=%d tasks=%d", got.MaxConcurrentAgents, got.MaxConcurrentTasks)
	}
}