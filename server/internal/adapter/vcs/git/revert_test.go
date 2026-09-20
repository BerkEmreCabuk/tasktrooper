package git

// Rollback mechanism (ii): revert the merge commit on the default branch and
// push.
//
// This is the rollback for a repository that has no deploy workflow to
// dispatch — a push-to-deploy host builds whatever the default branch points
// at, so the only thing that redeploys it is a new commit. It runs unattended,
// on the branch production is built from, which is why every one of these tests
// is about a way it could do damage rather than about the happy path.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRevertFixture builds a bare origin on main with a base commit and a
// "release" commit on top, plus the mirror clone the rollback runs in.
func newRevertFixture(t *testing.T) (root, origin, releaseSHA string) {
	t.Helper()
	base := t.TempDir()
	origin = filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	author := filepath.Join(base, "author")
	gitRun(t, base, "clone", origin, author)
	if err := os.WriteFile(filepath.Join(author, "app.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "base")
	gitRun(t, author, "push", "origin", "main")

	if err := os.WriteFile(filepath.Join(author, "app.txt"), []byte("v2 broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "T-7: ship v2")
	gitRun(t, author, "push", "origin", "main")
	releaseSHA = gitRun(t, author, "rev-parse", "HEAD")

	root = filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)
	return root, origin, releaseSHA
}

func TestRevertCommitOnDefaultBranchUndoesTheReleaseAndPushes(t *testing.T) {
	root, origin, releaseSHA := newRevertFixture(t)

	revertSHA, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, releaseSHA,
		"revert: roll back T-7 (ship v2)")
	if err != nil {
		t.Fatalf("RevertCommitOnDefaultBranch: %v", err)
	}
	if revertSHA == "" || revertSHA == releaseSHA {
		t.Fatalf("revert sha = %q, want the new commit that was actually created", revertSHA)
	}

	// The revert must be on ORIGIN — a rollback that only exists locally is a
	// production still running the bad release while the card says "rolled
	// back".
	pushed := gitRun(t, origin, "rev-parse", "main")
	if pushed != revertSHA {
		t.Fatalf("origin/main = %q, want the revert commit %q", pushed, revertSHA)
	}
	// And the content is actually back.
	content := gitRun(t, origin, "show", "main:app.txt")
	if strings.TrimSpace(content) != "v1" {
		t.Fatalf("app.txt on origin = %q, want the pre-release content", content)
	}
	// The custom message survives the amend.
	subject := gitRun(t, root, "log", "-1", "--pretty=%s")
	if !strings.Contains(subject, "roll back T-7") {
		t.Fatalf("commit subject = %q, want the rollback message", subject)
	}
}

// Nothing is force-pushed and nothing is rewritten: the release commit is still
// in origin's history, with the revert on top of it. A rollback that rewrote
// the branch would destroy every commit that landed after the one being undone.
func TestRevertCommitOnDefaultBranchDoesNotRewriteHistory(t *testing.T) {
	root, origin, releaseSHA := newRevertFixture(t)

	if _, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, releaseSHA, "revert"); err != nil {
		t.Fatalf("RevertCommitOnDefaultBranch: %v", err)
	}

	history := gitRun(t, origin, "log", "--pretty=%H", "main")
	if !strings.Contains(history, releaseSHA) {
		t.Fatalf("the reverted commit is gone from origin's history — this was a rewrite, not a revert:\n%s", history)
	}
	if count := len(strings.Fields(history)); count != 3 {
		t.Fatalf("origin/main has %d commits, want 3 (base, release, revert)", count)
	}
}

// A revert that conflicts means later commits touched the same lines. Guessing
// which side wins, unattended, on the branch production builds from, is not
// something to do — the tree is left clean and the caller is told.
func TestRevertCommitOnDefaultBranchAbortsOnConflict(t *testing.T) {
	root, origin, releaseSHA := newRevertFixture(t)

	// Somebody else edits the same line after the release.
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, filepath.Dir(other), "clone", origin, other)
	if err := os.WriteFile(filepath.Join(other, "app.txt"), []byte("v3 by somebody else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-m", "v3")
	gitRun(t, other, "push", "origin", "main")

	_, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, releaseSHA, "revert")
	if err == nil {
		t.Fatal("a conflicting revert must fail loudly, not resolve itself")
	}
	if !strings.Contains(err.Error(), "git revert") {
		t.Fatalf("err = %v, want it to name the failing operation", err)
	}
	// Nothing half-applied: `git revert --abort` restored the tree.
	if status := gitRun(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("the working tree was left dirty after a failed revert:\n%s", status)
	}
	// And origin is untouched.
	if head := gitRun(t, origin, "rev-parse", "main"); head != gitRun(t, other, "rev-parse", "HEAD") {
		t.Fatalf("origin/main moved despite the revert failing")
	}
}

// A ref that starts with '-' is an argument, not a commit. git parses its own
// argv, so there is no shell needed for this to be injection.
func TestRevertCommitOnDefaultBranchRefusesAnOptionLikeRef(t *testing.T) {
	root, _, _ := newRevertFixture(t)

	if _, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, "--help", "revert"); err == nil {
		t.Fatal("a ref beginning with '-' must be refused before it reaches git")
	}
	if _, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, "  ", "revert"); err == nil {
		t.Fatal("an empty ref must be refused")
	}
}

// A SHA that is not in this repository is refused before anything is committed.
func TestRevertCommitOnDefaultBranchRefusesAnUnknownCommit(t *testing.T) {
	root, _, _ := newRevertFixture(t)

	_, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root,
		"0123456789012345678901234567890123456789", "revert")
	if err == nil {
		t.Fatal("an unknown commit must be refused")
	}
	if !strings.Contains(err.Error(), "not a commit") {
		t.Fatalf("err = %v, want it to say the commit does not exist", err)
	}
}
