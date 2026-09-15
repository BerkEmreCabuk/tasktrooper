package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func newMirrorService(t *testing.T, repo domain.Repository, git *fakeRestoreGit) *Service {
	t.Helper()
	return &Service{
		repos:         &fakeReleaseRepoStore{repo: repo},
		git:           git,
		workspaceRoot: t.TempDir(),
		gitWarnings:   map[uuid.UUID]string{},
	}
}

// The case the whole guard exists for: this replica never served the import (or
// was restarted), so the checkout an index pass is about to walk is simply not
// there. Walking it would succeed, find no files, and complete — so the clone
// has to come back first.
func TestIndexMirrorIsRestoredWhenMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	repo := domain.Repository{ID: uuid.New(), Name: "app", RootPath: root, RemoteURL: "https://github.com/acme/app.git"}
	git := &fakeRestoreGit{}
	svc := newMirrorService(t, repo, git)

	if err := svc.EnsureIndexMirror(context.Background(), repo.ID, root); err != nil {
		t.Fatalf("restore refused a restorable mirror: %v", err)
	}
	if len(git.clones) != 1 {
		t.Fatalf("clones = %v, want exactly one", git.clones)
	}
	if !strings.Contains(git.clones[0], repo.RemoteURL) || !strings.Contains(git.clones[0], root) {
		t.Fatalf("cloned the wrong thing or to the wrong place: %q", git.clones[0])
	}
	if !git.HasGit(root) {
		t.Fatal("the restore reported success but left no working copy")
	}
}

// A mirror that is already there is left alone. It is refreshed by
// SyncDefaultBranch, not re-cloned: re-cloning a present checkout on every pass
// would turn each push into a full fetch of the repository.
func TestIndexMirrorPresentIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	repo := domain.Repository{ID: uuid.New(), Name: "app", RootPath: root, RemoteURL: "https://github.com/acme/app.git"}
	git := &fakeRestoreGit{repoPaths: map[string]bool{root: true}}
	svc := newMirrorService(t, repo, git)

	if err := svc.EnsureIndexMirror(context.Background(), repo.ID, root); err != nil {
		t.Fatalf("present mirror was refused: %v", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("a present mirror was re-cloned: %v", git.clones)
	}
}

// Nothing on record to restore from. The pass must stop with a sentence naming
// that, because the alternative — indexing an empty directory and reporting
// success — is the failure this whole path exists to prevent.
func TestIndexMirrorWithoutRemoteFailsLoudly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	repo := domain.Repository{ID: uuid.New(), Name: "app", RootPath: root}
	git := &fakeRestoreGit{}
	svc := newMirrorService(t, repo, git)

	err := svc.EnsureIndexMirror(context.Background(), repo.ID, root)
	if err == nil {
		t.Fatal("a repository with no remote_url was allowed to index nothing")
	}
	if !strings.Contains(err.Error(), "remote_url") {
		t.Fatalf("error %q does not name what is missing", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("cloned without a remote: %v", git.clones)
	}
}

// A folder that exists but is not a repository is somebody else's. It is not
// emptied, not deleted and not indexed: whatever is in it, an index built from
// it would describe something other than this repository.
func TestIndexMirrorRefusesANonRepositoryFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := domain.Repository{ID: uuid.New(), Name: "app", RootPath: root, RemoteURL: "https://github.com/acme/app.git"}
	git := &fakeRestoreGit{}
	svc := newMirrorService(t, repo, git)

	err := svc.EnsureIndexMirror(context.Background(), repo.ID, root)
	if err == nil {
		t.Fatal("a non-repository folder was accepted as a mirror")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error %q does not say what is in the way", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("cloned over somebody else's folder: %v", git.clones)
	}
	if _, statErr := os.Stat(filepath.Join(root, "notes.txt")); statErr != nil {
		t.Fatalf("the refused folder's contents were touched: %v", statErr)
	}
}

// A clone that fails is a failed pass, not a pass over whatever is on disk.
func TestIndexMirrorCloneFailureFailsThePass(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	repo := domain.Repository{ID: uuid.New(), Name: "app", RootPath: root, RemoteURL: "https://github.com/acme/app.git"}
	git := &fakeRestoreGit{cloneErr: errors.New("authentication failed")}
	svc := newMirrorService(t, repo, git)

	err := svc.EnsureIndexMirror(context.Background(), repo.ID, root)
	if err == nil {
		t.Fatal("a failed clone was reported as a usable mirror")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("error %q drops git's own words", err)
	}
}
