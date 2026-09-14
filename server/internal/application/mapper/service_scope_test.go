package mapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/require"
)

// FileSkeleton is the sink behind get_symbol_skeleton: relPath arrives from a
// tool argument and ends in os.ReadFile. The check lives here as well as in the
// tool so a future caller that forgets it still cannot leave the root.
func TestFileSkeletonRejectsPathsOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	outside := filepath.Join(base, "secrets")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "env.go"),
		[]byte("package secrets\n\nfunc Leak() {}\n"), 0o644))

	svc := NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50})

	for _, rel := range []string{
		"../secrets/env.go",
		"../../../../../../etc/passwd",
		"sub/../../secrets/env.go",
	} {
		_, err := svc.FileSkeleton(root, rel)
		require.Error(t, err, "expected %q to be rejected", rel)
		require.Contains(t, err.Error(), "outside the workspace root")
	}
}

func TestFileSkeletonStillReadsInsideRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "main.go"),
		[]byte("package pkg\n\nfunc Main() {}\n"), 0o644))

	svc := NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50})

	sk, err := svc.FileSkeleton(root, "pkg/main.go")
	require.NoError(t, err)
	require.Equal(t, "pkg", sk.Package)
	require.NotEmpty(t, sk.Symbols)
}
