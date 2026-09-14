package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// These are stat-level tests on purpose: Presence must not need a git binary,
// because it runs once per repository in every list response.

// worktreeDotGit writes the .git FILE a worktree or submodule checkout has,
// without creating one — the pointer's target is irrelevant to a stat.
func worktreeDotGit(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/tt-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPresenceDotGitDirectoryIsARepository(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := NewClient().Presence(root)
	if got.State != domain.GitPresenceRepository {
		t.Fatalf("State = %q, want %q", got.State, domain.GitPresenceRepository)
	}
	if w := got.Warning(); w != "" {
		t.Fatalf("Warning = %q, want none", w)
	}
	if !NewClient().HasGit(root) {
		t.Fatal("HasGit = false for a clone")
	}
}

// A worktree (and a submodule checkout) has .git as a FILE. Requiring IsDir()
// reported every one of them as "not a git repository", which is what the
// board showed for task workspaces.
func TestPresenceDotGitFileIsARepository(t *testing.T) {
	root := t.TempDir()
	worktreeDotGit(t, root)

	got := NewClient().Presence(root)
	if got.State != domain.GitPresenceRepository {
		t.Fatalf("State = %q, want %q", got.State, domain.GitPresenceRepository)
	}
	if w := got.Warning(); w != "" {
		t.Fatalf("Warning = %q, want none", w)
	}
	if !NewClient().HasGit(root) {
		t.Fatal("HasGit = false for a worktree checkout")
	}
}

func TestPresencePlainDirectoryHasNoRepository(t *testing.T) {
	root := t.TempDir()

	got := NewClient().Presence(root)
	if got.State != domain.GitPresenceNoRepository {
		t.Fatalf("State = %q, want %q", got.State, domain.GitPresenceNoRepository)
	}
	if want := "This project is not a git repository yet"; got.Warning() != want {
		t.Fatalf("Warning = %q, want %q", got.Warning(), want)
	}
	if NewClient().HasGit(root) {
		t.Fatal("HasGit = true for a directory with no .git")
	}
}

// The defect this whole distinction exists for: a root_path that does not
// resolve on the host currently serving the tenant.
func TestPresenceMissingPathIsNotReportedAsMissingGit(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")

	got := NewClient().Presence(missing)
	if got.State != domain.GitPresencePathMissing {
		t.Fatalf("State = %q, want %q", got.State, domain.GitPresencePathMissing)
	}
	warning := got.Warning()
	if !strings.Contains(warning, "was not found at its recorded path") {
		t.Fatalf("Warning = %q, want it to say the folder was not found", warning)
	}
	if strings.Contains(warning, "git repository") {
		t.Fatalf("Warning = %q, must not blame git for a folder that is not there", warning)
	}
	// The card prints root_path directly under this notice.
	if strings.Contains(warning, missing) {
		t.Fatalf("Warning = %q, must not repeat the path", warning)
	}
	if NewClient().HasGit(missing) {
		t.Fatal("HasGit = true for a path that does not exist")
	}
}

// An empty root path is the same class of problem: there is no folder to look
// in, and stat("/.git") would otherwise answer for the filesystem root.
func TestPresenceEmptyPathIsMissing(t *testing.T) {
	if got := NewClient().Presence("   "); got.State != domain.GitPresencePathMissing {
		t.Fatalf("State = %q, want %q", got.State, domain.GitPresencePathMissing)
	}
}

func TestPresenceUnreadablePathReportsTheReason(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny access")
	}
	root := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Search permission removed: the .git inside is genuinely there, and the
	// answer is still unobtainable. Restored so t.TempDir cleanup can run.
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	got := NewClient().Presence(root)
	if got.State != domain.GitPresenceUnreadable {
		t.Fatalf("State = %q, want %q", got.State, domain.GitPresenceUnreadable)
	}
	if got.Reason == "" {
		t.Fatal("Reason is empty; the user gets no way to tell a permissions problem from an I/O one")
	}
	if strings.Contains(got.Reason, root) {
		t.Fatalf("Reason = %q, must carry the error and not the path", got.Reason)
	}
	warning := got.Warning()
	if !strings.Contains(warning, "could not be read") || !strings.Contains(warning, got.Reason) {
		t.Fatalf("Warning = %q, want it to say the folder could not be read and why", warning)
	}
	if NewClient().HasGit(root) {
		t.Fatal("HasGit = true for a folder that cannot be read")
	}
}

// A root path that is a regular file stats as ENOTDIR on <root>/.git. It is
// not a repository and it is not a missing folder either.
func TestPresenceRootThatIsAFileIsUnreadable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := NewClient().Presence(root)
	if got.State != domain.GitPresenceUnreadable {
		t.Fatalf("State = %q, want %q", got.State, domain.GitPresenceUnreadable)
	}
	if got.Reason == "" {
		t.Fatal("Reason is empty")
	}
}

// Status carries the same distinctions: its Warning used to say "not a git
// repository" for every one of these.
func TestStatusWarningMatchesPresence(t *testing.T) {
	client := NewClient()
	ctx := context.Background()

	missing := filepath.Join(t.TempDir(), "gone")
	st := client.Status(ctx, missing)
	if st.Initialized {
		t.Fatal("Initialized = true for a path that does not exist")
	}
	if want := client.Presence(missing).Warning(); st.Warning != want {
		t.Fatalf("Warning = %q, want %q", st.Warning, want)
	}

	plain := t.TempDir()
	st = client.Status(ctx, plain)
	if st.Initialized {
		t.Fatal("Initialized = true for a directory with no .git")
	}
	if want := "This project is not a git repository yet"; st.Warning != want {
		t.Fatalf("Warning = %q, want %q", st.Warning, want)
	}
}
