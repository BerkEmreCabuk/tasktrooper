package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// EnsureIndexMirror guarantees that the clone an index pass is about to walk is
// actually on this process's filesystem, and fails loudly when it cannot be.
//
// ── Why this server holds a clone at all ────────────────────────────────────
//
// Nothing else here does. Agent tools run on the assignee's Mac, and a board
// run prepares its workspace there too (board/remote_workspace.go); this server
// holds the board, the database and the orchestration, not the code. The
// indexer is the exception, and the reason is the shape of what it needs: FILE
// CONTENT, all of it, for every source file in the tree, to chunk and embed.
//
// That content is bulk, immutable per commit, and already served cheaply by
// GitHub. Fetching it here is one datacentre-to-datacentre fetch of the delta
// since the last pass. The alternative — reading each file through the reverse
// tunnel to the user's Mac — would put a round trip across a domestic
// connection in front of every file in the repository, to obtain bytes the
// cloud can fetch directly and which are not private to that machine in the
// first place. An index pass would spend its wall clock on the slowest link in
// the system for no gain.
//
// The part of indexing that genuinely must reach the Mac is the EMBEDDING call,
// because the model runs there and nowhere else, and that is already wired
// (adapter/llm/runner_embed.go). Content comes from GitHub; vectors come from
// the Mac. This clone is the first half of that split and has no other user:
// it is refreshed by SyncDefaultBranch before each pass and read by the walk.
//
// ── Why it has to be restored rather than assumed ───────────────────────────
//
// It is a CACHE on an ephemeral pod disk, behind N replicas, while the index
// rows it produces live in shared Postgres. A pod restart, a rescheduled
// replica, or a pass that simply lands on a replica which never served the
// import all find root_path empty — with the index rows looking perfectly
// healthy. Without this step the pass would walk nothing, embed nothing, and
// write "completed": a silently empty index, which is worse than a failed one,
// because nothing looks wrong until an agent's code search comes back empty and
// the agent concludes the code does not exist.
//
// So the refusals below are refusals, not fallbacks. They are deliberately the
// same ones the board runner applies before starting an agent
// (board/runner.go:ensureWorkingCopy) — a directory that exists but is not a
// repository is not ours to delete, a repository with no recorded remote cannot
// be restored by anything this process knows, and a checkout of a DIFFERENT
// repository is not ours to index whatever else is true of it.
func (s *Service) EnsureIndexMirror(ctx context.Context, repositoryID uuid.UUID, rootPath string) error {
	if s == nil {
		return nil
	}
	if s.git == nil {
		// No git client is a deployment that could not restore anything anyway,
		// and one that never cloned this checkout either. Left alone: the walk
		// still fails on a missing directory, which is the honest error there.
		return nil
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("the code at %s is missing and the repository record could not be read to restore it: %w", rootPath, err)
	}
	if s.git.HasGit(rootPath) {
		// "There is a repository here" is not "there is THIS repository here",
		// and this is the one adoption path that needs no user action at all: a
		// webhook or a freshness check reaches it on its own. Answering the
		// weaker question is what let an index pass walk a checkout it had no
		// business reading and write its contents into the wrong repository's
		// chunks. The record is read BEFORE the shortcut for that reason.
		return s.assertSameRepo(ctx, rootPath, repo.RemoteURL)
	}
	remote := strings.TrimSpace(repo.RemoteURL)
	if remote == "" {
		return fmt.Errorf("repository %q has no clone at %s and no remote_url on record to restore it from — re-import it from GitHub "+
			"(or set its remote) before it can be indexed", repo.Name, rootPath)
	}
	if entries, readErr := os.ReadDir(rootPath); readErr == nil && len(entries) > 0 {
		// Non-empty but not a repository: cloning into it would fail anyway,
		// and deleting a directory this process did not create is not its call
		// to make. Whatever is in there, a person has to look at it.
		return fmt.Errorf("repository %q root %s exists but is not a git repository; refusing to index it, because indexing whatever "+
			"is in that folder would produce an index that describes something other than this repository", repo.Name, rootPath)
	}
	// One clone at a time per repository. The explicit "restore working copy"
	// button writes into the same directory through the same ledger, and two
	// clones racing into one path leave a half-written tree that HasGit reports
	// as fine.
	if s.IsRestoreRunning(repositoryID) {
		return fmt.Errorf("repository %q is being restored to this server right now; indexing will pick it up on the next pass", repo.Name)
	}

	log.Info().
		Str("repository_id", repositoryID.String()).
		Str("repository", repo.Name).
		Str("root", rootPath).
		Msg("index mirror is missing on this replica; restoring it from origin before indexing")

	s.recordRestore(repositoryID, &domain.RepositoryRestore{
		Status: domain.RepositoryRestoreRunning, RootPath: rootPath, StartedAt: time.Now(),
	})
	cctx, cancel := context.WithTimeout(ctx, restoreCloneTimeout)
	defer cancel()
	// The same call the GitHub import makes, so a private repository works for
	// the same reason it does there: the adapter injects the stored GitHub
	// token (git.Client.TokenSource). There is deliberately no second
	// authentication path.
	if cloneErr := s.git.CloneRepo(cctx, remote, rootPath); cloneErr != nil {
		err := fmt.Errorf("restoring the clone of %q from %s failed, so there was nothing to index: %w", repo.Name, remote, cloneErr)
		s.finishRestore(repositoryID, rootPath, err)
		return err
	}
	if !s.git.HasGit(rootPath) {
		err := fmt.Errorf("restoring the clone of %q reported success but %s is still not a git repository", repo.Name, rootPath)
		s.finishRestore(repositoryID, rootPath, err)
		return err
	}
	s.finishRestore(repositoryID, rootPath, nil)
	log.Info().Str("repository_id", repositoryID.String()).Str("root", rootPath).
		Msg("index mirror restored; indexing continues")
	return nil
}
