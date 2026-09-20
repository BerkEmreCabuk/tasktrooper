package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"

	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
)

// wireRepositoryStore is the composition buildHandler used to skip: construct
// the store, then never call SetCipher on it, leaving every webhook install
// to derive one lazily on first use — always after scrubProcessSecrets has
// wiped MCP_SECRETS_KEY. These drive the wired store through SetWebhook,
// which fails on the cipher before it ever touches the pool: that failure
// shape (a cipher error vs. a pool error) can only tell the two cases apart
// if the injected cipher/cipherErr actually reached the store through this
// composition, not just through RepositoryStore.SetCipher in isolation.
func TestWireRepositoryStoreInjectsTheBootCipher(t *testing.T) {
	cfg := &domain.Config{}
	cipher, err := secrets.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	// Unreachable on purpose: SetWebhook must get past the cipher check and
	// fail on the pool instead, which is only possible if the cipher above
	// actually arrived through the wiring.
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/db?sslmode=disable")
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store := wireRepositoryStore(pgstore.NewDB(pool), cfg, cipher, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = store.SetWebhook(ctx, uuid.New(), "secret", 1)
	if err == nil {
		t.Fatal("expected an error from the unreachable pool, got nil")
	}
	if !strings.Contains(err.Error(), "set repository webhook") {
		t.Fatalf("SetWebhook error = %q, want the pool-exec failure — the injected cipher should have let it get that far", err)
	}
}

func TestWireRepositoryStorePropagatesABootCipherFailure(t *testing.T) {
	cfg := &domain.Config{}
	bootErr := errors.New("boot cipher unavailable")

	// The nil pool is deliberate: with the boot cipher error correctly wired,
	// SetWebhook must fail on the cipher and never reach it.
	store := wireRepositoryStore(nil, cfg, nil, bootErr)

	err := store.SetWebhook(context.Background(), uuid.New(), "secret", 1)
	if !errors.Is(err, bootErr) {
		t.Fatalf("SetWebhook error = %v, want %v (the boot cipher error, returned before the nil pool is ever touched)", err, bootErr)
	}
}
