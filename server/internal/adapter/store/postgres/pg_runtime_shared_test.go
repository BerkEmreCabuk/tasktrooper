package postgres_test

import (
	"os"
	"testing"
)

// sharedPGRuntimeDir is where every suite in this package extracts the
// embedded-postgres binary bundle. Each suite still gets its own DataDir — a
// fresh, empty database, so suites cannot see each other's rows — but the
// binary extraction itself is the same ~30MB archive 18+ times over, and that
// is what turned a full package run into something long enough to hit an
// external caller's own timeout. The per-suite RuntimePath this replaced
// existed to stop two *separate test binaries* from extracting into the same
// cache at once (a real race in `go test ./...`); suites within one package
// run sequentially in one binary, so sharing this path between them races
// nothing.
var sharedPGRuntimeDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tasktrooper-pg-runtime-")
	if err != nil {
		panic(err)
	}
	sharedPGRuntimeDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
