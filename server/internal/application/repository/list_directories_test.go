package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestServiceListDirectories(t *testing.T) {
	root := monorepoLayout(t)
	// A noise directory alongside the real sub-projects: must never appear.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", "left-pad"), 0o755))
	svc := &Service{repos: &fakeReleaseRepoStore{repo: domain.Repository{RootPath: root}}}

	t.Run("root listing skips noise directories", func(t *testing.T) {
		path, kind, entries, err := svc.ListDirectories(context.Background(), uuid.New(), "")
		require.NoError(t, err)
		require.Equal(t, "", path)
		require.Equal(t, domain.RepoKindBackend, kind)
		require.Equal(t, []string{"apps"}, entries)
	})

	t.Run("descending into a child returns its children", func(t *testing.T) {
		path, kind, entries, err := svc.ListDirectories(context.Background(), uuid.New(), "apps")
		require.NoError(t, err)
		require.Equal(t, "apps", path)
		require.Equal(t, domain.RepoKindBackend, kind)
		require.ElementsMatch(t, []string{"api", "web"}, entries)
	})

	t.Run("a leaf directory's own kind is detected", func(t *testing.T) {
		path, kind, entries, err := svc.ListDirectories(context.Background(), uuid.New(), "apps/api")
		require.NoError(t, err)
		require.Equal(t, "apps/api", path)
		require.Equal(t, domain.RepoKindBackend, kind)
		require.Empty(t, entries)

		_, kind, _, err = svc.ListDirectories(context.Background(), uuid.New(), "apps/web")
		require.NoError(t, err)
		require.Equal(t, domain.RepoKindFrontend, kind)
	})

	t.Run("escaping the repository root is refused", func(t *testing.T) {
		_, _, _, err := svc.ListDirectories(context.Background(), uuid.New(), "../../etc")
		require.Error(t, err)
	})
}
