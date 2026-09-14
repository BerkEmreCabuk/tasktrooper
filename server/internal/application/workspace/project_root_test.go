package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/stretchr/testify/require"
)

// tenantRootDir makes the calling tenant's subtree on disk and returns it.
func tenantRootDir(t *testing.T, wsRoot string, id [16]byte) string {
	t.Helper()
	dir, err := workspace.TenantRoot(ctxFor(id), wsRoot)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func TestValidateProjectRoot_InsideOwnTenantSubtree(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	repo := filepath.Join(tenantRootDir(t, wsRoot, tenantA), "repos", "api")
	require.NoError(t, os.MkdirAll(repo, 0o755))

	root, err := workspace.ValidateProjectRoot(ctxFor(tenantA), repo, wsRoot, nil)
	require.NoError(t, err)
	require.Equal(t, mustAbs(repo), root)
}

// An empty allowed_roots is the shipped cloud configuration. It used to mean
// "any absolute path on the pod", which is what made POST /v1/repositories/open
// a one-request read of another customer's checkout.
func TestValidateProjectRoot_EmptyAllowlistRefusesArbitraryPaths(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	tenantRootDir(t, wsRoot, tenantA)
	outside := t.TempDir()

	_, err := workspace.ValidateProjectRoot(ctxFor(tenantA), outside, wsRoot, nil)
	require.Error(t, err)
	// The refusal must not confirm the directory exists: on a shared volume
	// that difference enumerates other customers' repository names.
	require.NotContains(t, err.Error(), outside)
}

func TestValidateProjectRoot_RefusesAnotherTenantsSubtree(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	victim := filepath.Join(tenantRootDir(t, wsRoot, tenantB), "repos", "api")
	require.NoError(t, os.MkdirAll(victim, 0o755))
	tenantRootDir(t, wsRoot, tenantA)

	_, err := workspace.ValidateProjectRoot(ctxFor(tenantA), victim, wsRoot, nil)
	require.Error(t, err)
}

// allowed_roots only ever WIDENS the set, for the self-hosted install that
// keeps its checkouts outside the managed workspace.
func TestValidateProjectRoot_AllowedRootWidens(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	outside := t.TempDir()

	root, err := workspace.ValidateProjectRoot(ctxFor(tenantA), outside, wsRoot, []string{outside})
	require.NoError(t, err)
	require.Equal(t, mustAbs(outside), root)
}

func TestValidateProjectRoot_NoTenantIsRefused(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	dir := t.TempDir()

	_, err := workspace.ValidateProjectRoot(context.Background(), dir, wsRoot, []string{dir})
	require.Error(t, err)
}

func TestValidateProjectRoot_NotDirectory(t *testing.T) {
	f, err := os.CreateTemp("", "file-*")
	require.NoError(t, err)
	_ = f.Close()
	_, err = workspace.ValidateProjectRoot(ctxFor(tenantA), f.Name(), t.TempDir(), nil)
	require.Error(t, err)
}

func mustAbs(path string) string {
	abs, _ := filepath.Abs(path)
	return abs
}
