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

// fakeRestoreGit records what a restore asked git to do. Presence is answered
// per path so the recorded root path and the clone destination can differ —
// which is the whole point of a restore.
type fakeRestoreGit struct {
	fakeReleaseGit
	// presenceByPath overrides Presence for specific paths; anything else
	// falls back to a real stat, so a t.TempDir() destination answers honestly.
	presenceByPath map[string]domain.GitPresence
	clones         []string
	cloneErr       error
	// repoPaths are the paths HasGit answers true for (the destination-already-
	// holds-a-checkout case).
	repoPaths map[string]bool
}

func (f *fakeRestoreGit) Presence(path string) domain.GitPresence {
	if p, ok := f.presenceByPath[path]; ok {
		return p
	}
	if _, err := os.Stat(path); err != nil {
		return domain.GitPresence{State: domain.GitPresencePathMissing}
	}
	if f.repoPaths[path] {
		return domain.GitPresence{State: domain.GitPresenceRepository}
	}
	return domain.GitPresence{State: domain.GitPresenceNoRepository}
}

func (f *fakeRestoreGit) HasGit(path string) bool { return f.Presence(path).IsRepository() }

func (f *fakeRestoreGit) CloneRepo(_ context.Context, cloneURL, dest string) error {
	f.clones = append(f.clones, cloneURL+" -> "+dest)
	if f.cloneErr != nil {
		return f.cloneErr
	}
	// A real clone leaves a working copy behind; so does this one, so the
	// destination guards see what they would see in production.
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		return err
	}
	if f.repoPaths == nil {
		f.repoPaths = map[string]bool{}
	}
	f.repoPaths[dest] = true
	return nil
}

// newRestoreService wires a service whose restore runs inline, so the outcome
// of the "background" half can be asserted without waiting on a goroutine.
func newRestoreService(t *testing.T, repo domain.Repository, git *fakeRestoreGit) (*Service, *fakeReleaseRepoStore, string) {
	t.Helper()
	workspaceRoot := t.TempDir()
	store := &fakeReleaseRepoStore{repo: repo}
	svc := &Service{
		repos:         store,
		git:           git,
		workspaceRoot: workspaceRoot,
		gitWarnings:   map[uuid.UUID]string{},
		restoreRun:    func(fn func()) { fn() },
	}
	return svc, store, workspaceRoot
}

func missingRepo() domain.Repository {
	return domain.Repository{
		ID:   uuid.New(),
		Name: "app",
		// A path written by a runtime this install used to run on.
		RootPath:  "/data/workspaces/repos/app",
		RemoteURL: "https://github.com/acme/app.git",
	}
}

// The case the whole feature is for: the folder is gone because the data
// moved to another machine, the remote is on the record, so the code is fetched
// into THIS runtime's layout and the row is re-pointed at where it landed.
func TestRestoreClonesMissingWorkingCopyIntoThisRuntimesWorkspace(t *testing.T) {
	repo := missingRepo()
	git := &fakeRestoreGit{presenceByPath: map[string]domain.GitPresence{
		repo.RootPath: {State: domain.GitPresencePathMissing},
	}}
	svc, store, workspaceRoot := newRestoreService(t, repo, git)

	got, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err != nil {
		t.Fatalf("RestoreWorkingCopy: %v", err)
	}

	want := filepath.Join(workspaceRoot, "repos", "app")
	if len(git.clones) != 1 || !strings.HasSuffix(git.clones[0], "-> "+want) {
		t.Fatalf("clones = %v, want one clone into %s", git.clones, want)
	}
	if !strings.HasPrefix(git.clones[0], repo.RemoteURL+" ") {
		t.Fatalf("cloned from %q, want the recorded remote %q", git.clones[0], repo.RemoteURL)
	}
	if len(store.rootPathWrites) != 1 || store.rootPathWrites[0] != want {
		t.Fatalf("root_path writes = %v, want [%s]", store.rootPathWrites, want)
	}
	// The stored path is NOT reused: it belonged to the machine that no longer
	// has the code.
	if store.rootPathWrites[0] == repo.RootPath {
		t.Fatal("restore re-used the recorded path instead of this runtime's layout")
	}
	if got.GitRestore == nil || got.GitRestore.Status != domain.RepositoryRestoreCompleted {
		t.Fatalf("git_restore = %+v, want completed", got.GitRestore)
	}
}

// A repository whose folder is right there must never be cloned over: whatever
// is uncommitted in it is the one copy that exists.
func TestRestoreRefusedWhenWorkingCopyIsPresent(t *testing.T) {
	repo := missingRepo()
	git := &fakeRestoreGit{presenceByPath: map[string]domain.GitPresence{
		repo.RootPath: {State: domain.GitPresenceRepository},
	}}
	svc, store, _ := newRestoreService(t, repo, git)

	_, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err == nil {
		t.Fatal("restore was allowed over an existing working copy")
	}
	if len(git.clones) != 0 {
		t.Fatalf("clones = %v, want none", git.clones)
	}
	if len(store.rootPathWrites) != 0 {
		t.Fatalf("root_path was rewritten on a refused restore: %v", store.rootPathWrites)
	}
}

// Without a recorded remote there is nothing to fetch from, so the offer must
// not be made and the call must not pretend to do anything.
func TestRestoreRefusedWithoutARemote(t *testing.T) {
	repo := missingRepo()
	repo.RemoteURL = ""
	git := &fakeRestoreGit{presenceByPath: map[string]domain.GitPresence{
		repo.RootPath: {State: domain.GitPresencePathMissing},
	}}
	svc, store, _ := newRestoreService(t, repo, git)

	_, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err == nil || !strings.Contains(err.Error(), "No git remote is recorded") {
		t.Fatalf("err = %v, want a refusal naming the missing remote", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("clones = %v, want none", git.clones)
	}
	if len(store.rootPathWrites) != 0 {
		t.Fatalf("root_path was rewritten on a refused restore: %v", store.rootPathWrites)
	}
}

// A path holding something that is not a repository is somebody's data. The
// restore refuses, says what is in the way, and leaves every byte alone.
func TestRestoreRefusesNonRepositoryPathAndDeletesNothing(t *testing.T) {
	repo := missingRepo()
	occupied := t.TempDir()
	marker := filepath.Join(occupied, "notes.txt")
	if err := os.WriteFile(marker, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo.RootPath = occupied
	git := &fakeRestoreGit{}
	svc, store, _ := newRestoreService(t, repo, git)

	_, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err == nil {
		t.Fatal("restore was allowed onto a folder that is not a repository")
	}
	if !strings.Contains(err.Error(), "Nothing was changed or deleted") {
		t.Fatalf("err = %v, want it to promise nothing was deleted", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("clones = %v, want none", git.clones)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("the occupying file was removed: %v", statErr)
	}
	if len(store.rootPathWrites) != 0 {
		t.Fatalf("root_path was rewritten on a refused restore: %v", store.rootPathWrites)
	}
}

// The DESTINATION gets the same protection as the recorded path: a partial
// clone left by an earlier failure, or an unrelated folder of the same name, is
// reported rather than cleared.
func TestRestoreRefusesOccupiedDestinationAndDeletesNothing(t *testing.T) {
	repo := missingRepo()
	git := &fakeRestoreGit{presenceByPath: map[string]domain.GitPresence{
		repo.RootPath: {State: domain.GitPresencePathMissing},
	}}
	svc, store, workspaceRoot := newRestoreService(t, repo, git)

	dest := filepath.Join(workspaceRoot, "repos", "app")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dest, "half-downloaded")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err == nil || !strings.Contains(err.Error(), "Nothing was changed or deleted") {
		t.Fatalf("err = %v, want a refusal that promises nothing was deleted", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("clones = %v, want none", git.clones)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("the occupying file was removed: %v", statErr)
	}
	if len(store.rootPathWrites) != 0 {
		t.Fatalf("root_path was rewritten on a refused restore: %v", store.rootPathWrites)
	}
}

// A destination that already holds a working copy is adopted, not re-cloned:
// the code is here, only the record was stale.
func TestRestoreAdoptsAnExistingCheckoutAtTheDestination(t *testing.T) {
	repo := missingRepo()
	git := &fakeRestoreGit{presenceByPath: map[string]domain.GitPresence{
		repo.RootPath: {State: domain.GitPresencePathMissing},
	}}
	svc, store, workspaceRoot := newRestoreService(t, repo, git)

	dest := filepath.Join(workspaceRoot, "repos", "app")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	git.repoPaths = map[string]bool{dest: true}

	got, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err != nil {
		t.Fatalf("RestoreWorkingCopy: %v", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("clones = %v, want none — the checkout was already there", git.clones)
	}
	if len(store.rootPathWrites) != 1 || store.rootPathWrites[0] != dest {
		t.Fatalf("root_path writes = %v, want [%s]", store.rootPathWrites, dest)
	}
	if got.RootPath != dest {
		t.Fatalf("returned root_path = %q, want %q", got.RootPath, dest)
	}
}

// A clone that fails must say what git said and must not re-point the row at a
// directory that has no code in it.
func TestRestoreReportsCloneFailureAndLeavesTheRecordAlone(t *testing.T) {
	repo := missingRepo()
	git := &fakeRestoreGit{
		presenceByPath: map[string]domain.GitPresence{repo.RootPath: {State: domain.GitPresencePathMissing}},
		cloneErr:       errors.New("git clone: authentication failed"),
	}
	svc, store, _ := newRestoreService(t, repo, git)

	if _, err := svc.RestoreWorkingCopy(context.Background(), repo.ID); err != nil {
		t.Fatalf("starting the restore should succeed; the failure is reported on the state: %v", err)
	}
	state := svc.restoreState(repo.ID)
	if state == nil || state.Status != domain.RepositoryRestoreFailed {
		t.Fatalf("git_restore = %+v, want failed", state)
	}
	if !strings.Contains(state.Error, "authentication failed") {
		t.Fatalf("error = %q, want git's own words", state.Error)
	}
	if len(store.rootPathWrites) != 0 {
		t.Fatalf("root_path was rewritten after a failed clone: %v", store.rootPathWrites)
	}
}

// The card reads git_restorable, so the same rule the service enforces has to
// be the one the client is told — otherwise a button appears that only ever
// produces a 400.
func TestWithGitWarningMarksRestorabilityForTheCard(t *testing.T) {
	remote := "https://github.com/acme/app.git"
	cases := []struct {
		name      string
		presence  domain.GitPresence
		remoteURL string
		want      bool
	}{
		{"missing with remote", domain.GitPresence{State: domain.GitPresencePathMissing}, remote, true},
		{"missing without remote", domain.GitPresence{State: domain.GitPresencePathMissing}, "", false},
		{"present", domain.GitPresence{State: domain.GitPresenceRepository}, remote, false},
		{"not a repository", domain.GitPresence{State: domain.GitPresenceNoRepository}, remote, false},
		{"unreadable", domain.GitPresence{State: domain.GitPresenceUnreadable, Reason: "permission denied"}, remote, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			presence := tc.presence
			svc := &Service{git: &fakeReleaseGit{presence: &presence}}
			got := svc.withGitWarning(domain.Repository{ID: uuid.New(), RootPath: "/data/workspaces/repos/app", RemoteURL: tc.remoteURL})
			if got.GitRestorable != tc.want {
				t.Fatalf("GitRestorable = %v, want %v", got.GitRestorable, tc.want)
			}
		})
	}
}

// A repository whose git setup failed still shows that sentence, and is still
// restorable when its folder is missing and its origin is known — the two
// answers are about different things.
func TestRecordedSetupFailureDoesNotHideARestorableRepository(t *testing.T) {
	id := uuid.New()
	svc := &Service{
		git:         &fakeReleaseGit{presence: &domain.GitPresence{State: domain.GitPresencePathMissing}},
		gitWarnings: map[uuid.UUID]string{id: "Git/GitHub setup failed: boom"},
	}

	got := svc.withGitWarning(domain.Repository{ID: id, RootPath: "/data/workspaces/repos/app", RemoteURL: "https://github.com/acme/app.git"})

	if got.GitWarning != "Git/GitHub setup failed: boom" {
		t.Fatalf("GitWarning = %q, want the recorded setup failure", got.GitWarning)
	}
	if !got.GitRestorable {
		t.Fatal("a missing folder with a known remote was not offered a restore")
	}
}

// The directory name decides where the code lands, so a name that could climb
// out of the workspace root has to be rejected rather than cleaned up.
func TestRestoreDirNameFallsBackAndRefusesTraversal(t *testing.T) {
	cases := []struct {
		name string
		repo domain.Repository
		want string
	}{
		{
			name: "recorded path's last segment wins",
			repo: domain.Repository{RootPath: "/data/workspaces/repos/app", RemoteURL: "https://github.com/acme/other.git", Name: "display"},
			want: "app",
		},
		{
			name: "no path — the remote names it",
			repo: domain.Repository{RemoteURL: "https://github.com/acme/other.git", Name: "display"},
			want: "other",
		},
		{
			name: "scp-style remote",
			repo: domain.Repository{RemoteURL: "git@github.com:acme/other.git"},
			want: "other",
		},
		{
			name: "nothing but a display name",
			repo: domain.Repository{Name: "display"},
			want: "display",
		},
		{
			name: "a display name that would escape the workspace root is refused",
			repo: domain.Repository{Name: "../../etc"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := restoreDirName(tc.repo); got != tc.want {
				t.Fatalf("restoreDirName = %q, want %q", got, tc.want)
			}
		})
	}
}

// Without a workspace root there is nowhere this runtime may write, and
// guessing one would put a clone somewhere nobody asked for.
func TestRestoreNeedsAWorkspaceRoot(t *testing.T) {
	repo := missingRepo()
	svc := &Service{
		repos:       &fakeReleaseRepoStore{repo: repo},
		git:         &fakeRestoreGit{presenceByPath: map[string]domain.GitPresence{repo.RootPath: {State: domain.GitPresencePathMissing}}},
		gitWarnings: map[uuid.UUID]string{},
		restoreRun:  func(fn func()) { fn() },
	}

	_, err := svc.RestoreWorkingCopy(context.Background(), repo.ID)
	if err == nil || !strings.Contains(err.Error(), "workspace root") {
		t.Fatalf("err = %v, want a refusal naming the missing workspace root", err)
	}
}
