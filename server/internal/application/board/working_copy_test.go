package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// workingCopyGit is a port.GitClient that only implements what
// ensureWorkingCopy touches; every other method panics so a future change that
// starts calling one is caught instead of silently passing.
type workingCopyGit struct {
	port.GitClient
	cloneCalls []string
	cloneErr   error
	// cloneCreatesRepo controls whether a "successful" clone actually leaves a
	// git repo behind — the real failure mode this guards against.
	cloneCreatesRepo bool
	// origin is what the checkout already at the root reports. Empty means "no
	// origin recorded", which is not a mismatch.
	origin string
}

func (g *workingCopyGit) OriginURL(context.Context, string) string { return g.origin }

// HasGit mirrors the adapter: .git is a directory in a clone and a FILE in a
// worktree or submodule checkout, and both are repositories.
func (g *workingCopyGit) HasGit(rootPath string) bool {
	info, err := os.Stat(filepath.Join(rootPath, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func (g *workingCopyGit) CloneRepo(_ context.Context, cloneURL, dest string) error {
	g.cloneCalls = append(g.cloneCalls, cloneURL+" -> "+dest)
	if g.cloneErr != nil {
		return g.cloneErr
	}
	if g.cloneCreatesRepo {
		return os.MkdirAll(filepath.Join(dest, ".git"), 0o755)
	}
	return nil
}

func newWorkingCopyRunner(g port.GitClient) *Runner {
	return &Runner{git: g}
}

func TestEnsureWorkingCopy_ExistingRepoIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	g := &workingCopyGit{cloneCreatesRepo: true, origin: "git@github.com:acme/acme-web.git"}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.NoError(t, err)
	assert.Empty(t, g.cloneCalls, "an intact working copy must not be re-cloned")
}

func TestEnsureWorkingCopy_RestoresMissingCopyFromRemote(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme-web")
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.NoError(t, err)
	require.Len(t, g.cloneCalls, 1)
	assert.Contains(t, g.cloneCalls[0], "https://github.com/acme/acme-web.git")
}

func TestEnsureWorkingCopy_EmptyDirIsRestored(t *testing.T) {
	// The old runner called MkdirAll here and then ran the agent in the empty
	// directory it had just created.
	root := t.TempDir()
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.NoError(t, err)
	assert.Len(t, g.cloneCalls, 1)
}

func TestEnsureWorkingCopy_NoRemoteFailsLoudly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme-web")
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web"},
		root,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no remote_url on record")
	assert.Empty(t, g.cloneCalls)
	assert.NoDirExists(t, root, "a missing root must not be conjured into existence")
}

func TestEnsureWorkingCopy_NonEmptyNonRepoRefusesToRun(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o644))
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
	assert.Empty(t, g.cloneCalls)
}

func TestEnsureWorkingCopy_CloneThatLeavesNoRepoIsAnError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme-web")
	g := &workingCopyGit{cloneCreatesRepo: false}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "still not a git repository after clone")
}

// Reusing a checkout is adopting it. On a volume shared by every customer a
// directory with a .git in it is not evidence that it is THIS repository, and
// the run that follows commits and pushes — so a mismatch fails the run rather
// than working in somebody else's tree.
func TestEnsureWorkingCopy_RefusesADifferentRepository(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	g := &workingCopyGit{origin: "https://github.com/rival/acme-web.git"}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "rival", "the refusal must not name the other repository")
	assert.Empty(t, g.cloneCalls, "the refusal must not clone over the directory")
	assert.DirExists(t, root, "the refusal must not remove what it refused")
}
