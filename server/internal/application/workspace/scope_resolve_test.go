package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/stretchr/testify/require"
)

// filepath.Join Cleans rather than confines: it turns "../../etc/passwd" into a
// valid absolute path outside the root and hands it back without complaint.
// Every caller that joined a model-supplied path onto the workspace root was
// reading arbitrary files on the pod, so containment has to be proved after the
// join, not assumed from it.
func TestResolveWithinRootRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))

	for _, rel := range []string{
		"../",
		"../../etc/passwd",
		"../../../../../../etc/passwd",
		"pkg/../../outside",
		"pkg/../..",
		"./../secrets",
	} {
		_, err := workspace.ResolveWithinRoot(root, rel)
		require.Error(t, err, "expected %q to be rejected", rel)
		require.Contains(t, err.Error(), "outside the workspace root")
	}
}

// A checked-out repository can carry a symlink pointing anywhere. A textual
// prefix test on the unresolved path accepts it, so the comparison runs on the
// symlink-resolved forms of both sides.
func TestResolveWithinRootRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "passwd"), []byte("root:x:0:0"), 0o644))

	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))

	_, err := workspace.ResolveWithinRoot(root, "escape")
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink")

	_, err = workspace.ResolveWithinRoot(root, "escape/passwd")
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink")
}

// An absolute path is not an escape hatch either: Join re-roots it, so the
// caller names a path inside the workspace or nothing.
func TestResolveWithinRootReRootsAbsolutePaths(t *testing.T) {
	root := t.TempDir()

	resolved, err := workspace.ResolveWithinRoot(root, "/etc/passwd")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "etc", "passwd"), resolved)
}

func TestResolveWithinRootAllowsPathsInside(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "main.go"), []byte("package pkg"), 0o644))

	for rel, want := range map[string]string{
		"":                    root,
		".":                   root,
		"pkg":                 filepath.Join(root, "pkg"),
		"pkg/main.go":         filepath.Join(root, "pkg", "main.go"),
		"pkg/sub/../main.go":  filepath.Join(root, "pkg", "main.go"),
		"pkg/not-created-yet": filepath.Join(root, "pkg", "not-created-yet"),
	} {
		resolved, err := workspace.ResolveWithinRoot(root, rel)
		require.NoError(t, err, "expected %q to be accepted", rel)
		require.Equal(t, want, resolved)
	}
}

// An unset root must not degrade into "anything goes". It resolves to the
// process working directory, and a traversal out of that is still a traversal.
func TestResolveWithinRootWithEmptyRootStillRejectsTraversal(t *testing.T) {
	_, err := workspace.ResolveWithinRoot("", "../../../../../../etc/passwd")
	require.Error(t, err)
}
