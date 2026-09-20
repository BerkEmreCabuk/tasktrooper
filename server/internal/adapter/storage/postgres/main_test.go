package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// One cluster per test binary, one database per suite. Every suite used to
// start its own instance — an initdb, a server start and the whole migration
// history, 20 times over — which made the package slow enough to be killed by
// a caller's command timeout, and a killed run left its postgres behind.
// Started on first use so a -short run, where every suite skips, starts
// nothing.
var sharedPG struct {
	once    sync.Once
	dir     string
	cluster *database.Embedded
	err     error
}

func newTestDatabase(ctx context.Context) (*database.Embedded, error) {
	sharedPG.once.Do(func() {
		sharedPG.dir, sharedPG.err = os.MkdirTemp("", "tasktrooper-pg-")
		if sharedPG.err != nil {
			return
		}
		sharedPG.cluster, sharedPG.err = database.StartEmbedded(ctx, database.EmbeddedConfig{
			DataDir: filepath.Join(sharedPG.dir, "postgres"),
			// Its own path, not the library's shared cache: two test binaries
			// extracting into the same cache race each other in `go test ./...`.
			RuntimePath: filepath.Join(sharedPG.dir, "runtime"),
		})
	})
	if sharedPG.err != nil {
		return nil, sharedPG.err
	}
	return sharedPG.cluster.NewDatabase(ctx)
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedPG.cluster != nil {
		_ = sharedPG.cluster.Stop()
	}
	if sharedPG.dir != "" {
		_ = os.RemoveAll(sharedPG.dir)
	}
	os.Exit(code)
}
