package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/stretchr/testify/require"
)

func TestIsWithinRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "src", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(child), 0o755))
	require.NoError(t, os.WriteFile(child, []byte("x"), 0o644))

	ok, err := workspace.IsWithinRoot(child, root)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = workspace.IsWithinRoot("/tmp", root)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestResolveScopedWorkDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	def, err := workspace.ResolveScopedWorkDir("", root)
	require.NoError(t, err)
	require.Equal(t, root, def)

	rel, err := workspace.ResolveScopedWorkDir(sub, root)
	require.NoError(t, err)
	require.Equal(t, sub, rel)

	_, err = workspace.ResolveScopedWorkDir("/tmp", root)
	require.Error(t, err)
}
