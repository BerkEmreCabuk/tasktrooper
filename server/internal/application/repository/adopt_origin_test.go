package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// originGit answers OriginURL per path, which is what every adoption check
// turns on: two checkouts can both be repositories and only one of them can be
// the one on record.
type originGit struct {
	fakeRestoreGit
	origins map[string]string
}

func (g *originGit) OriginURL(_ context.Context, path string) string { return g.origins[path] }

// TestRepoPathRefusesATraversingName: the name is the repository's directory
// name, joined onto the workspace root, so one that could climb out of repos/
// is refused rather than sanitised.
func TestRepoPathRefusesATraversingName(t *testing.T) {
	root := t.TempDir()
	svc := &Service{workspaceRoot: root}

	got, err := svc.workspaceRepoPath("api")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "repos", "api"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if _, err := svc.workspaceRepoPath("../task-x"); err == nil {
		t.Fatal("a path-traversing repository name was accepted")
	}
}

// EnsureIndexMirror is the adoption path that needs no user action at all — a
// webhook or a freshness check reaches it. It used to return nil for any
// directory with a .git in it, so an index pass walked whatever was at the
// shared path and wrote its contents into this repository's chunks.
func TestIndexMirrorRefusesADifferentRepository(t *testing.T) {
	root := t.TempDir()
	repo := domain.Repository{
		ID: uuid.New(), Name: "api", RootPath: root,
		RemoteURL: "https://github.com/acme/api.git",
	}
	git := &originGit{
		fakeRestoreGit: fakeRestoreGit{repoPaths: map[string]bool{root: true}},
		origins:        map[string]string{root: "https://github.com/rival/api.git"},
	}
	svc := newMirrorService(t, repo, &git.fakeRestoreGit)
	svc.git = git

	err := svc.EnsureIndexMirror(context.Background(), repo.ID, root)
	if err == nil {
		t.Fatal("an index pass adopted a checkout of a different repository")
	}
	if strings.Contains(err.Error(), "rival") {
		t.Fatalf("the refusal disclosed the other repository: %v", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("the refusal cloned over somebody else's checkout: %v", git.clones)
	}
	if _, statErr := os.Stat(root); statErr != nil {
		t.Fatal("the refusal removed the directory it refused")
	}
}

// The same origin in either of git's two spellings is the same repository, and
// must still be adopted — a check that refused this would be turned off.
func TestIndexMirrorAdoptsTheSameRepositoryInAnotherSpelling(t *testing.T) {
	root := t.TempDir()
	repo := domain.Repository{
		ID: uuid.New(), Name: "api", RootPath: root,
		RemoteURL: "https://github.com/acme/api.git",
	}
	git := &originGit{
		fakeRestoreGit: fakeRestoreGit{repoPaths: map[string]bool{root: true}},
		origins:        map[string]string{root: "git@github.com:acme/api"},
	}
	svc := newMirrorService(t, repo, &git.fakeRestoreGit)
	svc.git = git

	if err := svc.EnsureIndexMirror(context.Background(), repo.ID, root); err != nil {
		t.Fatalf("the same repository in ssh spelling was refused: %v", err)
	}
}

// The restore button adopts a checkout that is already at the destination. Its
// comment enumerated two benign reasons a directory might be there and never
// considered a different customer's.
func TestRestoreRefusesToAdoptADifferentRepository(t *testing.T) {
	repo := missingRepo()
	git := &originGit{origins: map[string]string{}}
	svc, store, workspaceRoot := newRestoreService(t, repo, &git.fakeRestoreGit)
	svc.git = git

	dest := filepath.Join(workspaceRoot, "repos", "app")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	git.repoPaths = map[string]bool{dest: true}
	git.origins[dest] = "https://github.com/rival/app.git"

	_, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err == nil {
		t.Fatal("restore re-pointed a repository at a checkout of something else")
	}
	if len(store.rootPathWrites) != 0 {
		t.Fatalf("root_path was re-pointed anyway: %v", store.rootPathWrites)
	}
	if len(git.clones) != 0 {
		t.Fatalf("the refusal cloned over the directory: %v", git.clones)
	}
}
