package workspace_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
)

const localTenant = "00000000-0000-0000-0000-000000000001"

// fakePaths stands in for the database the way the postgres store behaves:
// only values containing a marker are offered, and each is replaced by value.
type fakePaths struct {
	values []string
}

func (f *fakePaths) RewriteStoredPaths(_ context.Context, markers []string, rewrite func(string) (string, bool)) (int, error) {
	changed := 0
	for i, v := range f.values {
		if !containsAny(v, markers) {
			continue
		}
		if flat, ok := rewrite(v); ok && flat != v {
			f.values[i] = flat
			changed++
		}
	}
	return changed, nil
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func mkfile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(path), 0o644))
}

// tree lists every path under root, so a rerun can be compared entry by entry.
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		out = append(out, rel)
		return nil
	}))
	sort.Strings(out)
	return out
}

func TestFlattenLegacyLayout(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "workspaces")
	legacy := filepath.Join(root, "tenants", localTenant)
	other := filepath.Join(root, "tenants", "22222222-2222-2222-2222-222222222222")
	moved := "task-" + uuid.NewString()
	blocked := "task-" + uuid.NewString()

	// The per-tenant layout.
	mkfile(t, filepath.Join(legacy, moved, "src", "main.go"))
	mkfile(t, filepath.Join(legacy, blocked, "old.txt"))
	mkfile(t, filepath.Join(legacy, "repos", "api", ".git", "HEAD"))
	mkfile(t, filepath.Join(legacy, "repos", "web", ".git", "HEAD"))
	mkfile(t, filepath.Join(legacy, "agent-cli", "claude", "agents", "dev.md"))
	mkfile(t, filepath.Join(other, "repos", "api", ".git", "HEAD"))
	// Already flat: repos/ is merged into; web and the blocked task collide.
	mkfile(t, filepath.Join(root, "repos", "docs", ".git", "HEAD"))
	mkfile(t, filepath.Join(root, "repos", "web", ".git", "HEAD"))
	mkfile(t, filepath.Join(root, blocked, "new.txt"))

	paths := &fakePaths{values: []string{
		filepath.Join(legacy, "repos", "api"),
		filepath.Join(legacy, moved),
		filepath.Join(legacy, "repos", "web"),
		"/data/workspaces/tenants/" + localTenant + "/task-abc",
		`C:\Users\me\TaskTrooper\workspaces\tenants\` + localTenant + `\repos\api`,
		filepath.Join(other, "repos", "api"),
		"/data/workspaces/tenants/" + localTenant + "0/repos/api",
	}}

	res, err := workspace.FlattenLegacyLayout(ctx, root, paths)
	require.NoError(t, err)
	require.Equal(t, 3, res.Moved, "the task, repos/api and agent-cli")
	require.Equal(t, 2, res.Conflicts, "repos/web and the blocked task")
	require.Equal(t, 4, res.Rewritten)

	// Moved whole, contents intact.
	require.FileExists(t, filepath.Join(root, moved, "src", "main.go"))
	require.FileExists(t, filepath.Join(root, "agent-cli", "claude", "agents", "dev.md"))
	// Merged one level into the existing repos/.
	require.FileExists(t, filepath.Join(root, "repos", "api", ".git", "HEAD"))
	require.FileExists(t, filepath.Join(root, "repos", "docs", ".git", "HEAD"))
	// Conflicts keep both sides, and a task checkout is never merged.
	require.FileExists(t, filepath.Join(legacy, "repos", "web", ".git", "HEAD"))
	require.FileExists(t, filepath.Join(legacy, blocked, "old.txt"))
	require.FileExists(t, filepath.Join(root, blocked, "new.txt"))
	require.NoFileExists(t, filepath.Join(root, blocked, "old.txt"))
	// Another tenant's directory is not touched.
	require.FileExists(t, filepath.Join(other, "repos", "api", ".git", "HEAD"))
	require.NoDirExists(t, filepath.Join(root, "repos", "api", "repos"))
	// Emptied sources are gone; the ones still holding a conflict stay.
	require.NoDirExists(t, filepath.Join(legacy, moved))
	require.NoDirExists(t, filepath.Join(legacy, "agent-cli"))
	require.NoDirExists(t, filepath.Join(legacy, "repos", "api"))
	require.DirExists(t, filepath.Join(legacy, "repos"))

	require.Equal(t, []string{
		filepath.Join(root, "repos", "api"),
		filepath.Join(root, moved),
		filepath.Join(legacy, "repos", "web"), // still exists: kept
		"/data/workspaces/task-abc",           // neither side exists: flat
		`C:\Users\me\TaskTrooper\workspaces\repos\api`,
		filepath.Join(other, "repos", "api"),
		"/data/workspaces/tenants/" + localTenant + "0/repos/api", // not the segment
	}, paths.values)

	// The next boot moves nothing and rewrites nothing.
	before := tree(t, root)
	valuesBefore := append([]string(nil), paths.values...)
	again, err := workspace.FlattenLegacyLayout(ctx, root, paths)
	require.NoError(t, err)
	require.Zero(t, again.Moved)
	require.Zero(t, again.Rewritten)
	require.Equal(t, before, tree(t, root))
	require.Equal(t, valuesBefore, paths.values)
}

func TestFlattenLegacyLayoutRemovesEmptiedDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	legacy := filepath.Join(root, "tenants", localTenant)
	mkfile(t, filepath.Join(legacy, "repos", "api", ".git", "HEAD"))
	require.NoError(t, os.MkdirAll(filepath.Join(legacy, "agents"), 0o755))
	mkfile(t, filepath.Join(root, "repos", "web", ".git", "HEAD"))

	res, err := workspace.FlattenLegacyLayout(context.Background(), root, nil)
	require.NoError(t, err)
	require.Equal(t, 2, res.Moved)
	require.Zero(t, res.Conflicts)
	require.NoDirExists(t, filepath.Join(root, "tenants"))
	require.FileExists(t, filepath.Join(root, "repos", "api", ".git", "HEAD"))
	require.FileExists(t, filepath.Join(root, "repos", "web", ".git", "HEAD"))
	require.DirExists(t, filepath.Join(root, "agents"))
}

func TestFlattenLegacyLayoutKeepsTenantsForAnotherTenant(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	other := filepath.Join(root, "tenants", uuid.NewString())
	mkfile(t, filepath.Join(root, "tenants", localTenant, "task-a", "f"))
	mkfile(t, filepath.Join(other, "task-b", "f"))

	res, err := workspace.FlattenLegacyLayout(context.Background(), root, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Moved)
	require.NoDirExists(t, filepath.Join(root, "tenants", localTenant))
	require.FileExists(t, filepath.Join(other, "task-b", "f"))
	require.NoDirExists(t, filepath.Join(root, "task-b"))
}

// A fresh install has no tenants/ at all, and an install with no workspace
// root has nothing to flatten; neither may create anything.
func TestFlattenLegacyLayoutNoLegacyLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	res, err := workspace.FlattenLegacyLayout(context.Background(), root, &fakePaths{})
	require.NoError(t, err)
	require.Equal(t, workspace.FlattenResult{}, res)
	require.NoDirExists(t, root)

	res, err = workspace.FlattenLegacyLayout(context.Background(), "  ", &fakePaths{values: []string{"/x/tenants/" + localTenant + "/repos/a"}})
	require.NoError(t, err)
	require.Equal(t, workspace.FlattenResult{}, res)
}
