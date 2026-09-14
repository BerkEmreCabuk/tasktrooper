package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// restoreCloneTimeout bounds one restore. It is generous on purpose: this is a
// full clone of a repository the user already owns, over whatever connection
// the machine running the tenant happens to have, and a monorepo on hotel wifi
// is not a hung process.
const restoreCloneTimeout = 30 * time.Minute

// RestoreWorkingCopy fetches a registered repository's code onto the machine
// that is running the tenant right now, then re-points root_path at where it
// actually landed.
//
// It exists because a tenant's runtime can move — a cloud pod today, the user's
// own Mac tomorrow — while the repository rows stay. Every row written by the
// previous runtime names a directory that does not exist here, which the board
// already says out loud (domain.GitPresence). What was missing was the next
// step: each of those rows also carries the remote it came from, so the code is
// one clone away and nothing needs to be re-imported by hand.
//
// It is EXPLICIT, one repository at a time, and never implicit in a read:
// a clone is minutes long and megabytes-to-gigabytes wide, and starting one
// because someone opened a page is how a board becomes unresponsive for ten
// minutes on a metered connection. See the note on IsRestoreRunning for what a
// bounded automatic caller would have to hold to.
//
// The HTTP handler does not wait for the clone. The claim, every refusal and
// the destination are decided synchronously — so a request that returns 202 has
// really started something — and only the network part runs in the background,
// reported through Repository.GitRestore on any later read of the repository.
func (s *Service) RestoreWorkingCopy(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.Repository{}, err
	}
	if s.git == nil {
		return domain.Repository{}, fmt.Errorf("git client is not configured")
	}
	// A restore already in flight is not an error and must not start a second
	// clone into the same directory: the caller gets the running attempt back.
	if s.IsRestoreRunning(repositoryID) {
		return s.withGitWarning(repo), nil
	}
	// The four-state presence answers this, and it is not re-derived here: a
	// folder that exists must never be clobbered, a folder that is not a
	// repository is not ours to remove, and an unreadable path is not a
	// licence to act. See domain.CanRestoreWorkingCopy.
	if ok, why := domain.CanRestoreWorkingCopy(s.git.Presence(repo.RootPath), repo.RemoteURL); !ok {
		return domain.Repository{}, fmt.Errorf("%s", why)
	}
	dest, err := s.restoreDestination(ctx, repo)
	if err != nil {
		return domain.Repository{}, err
	}
	// The destination gets the same courtesy as the recorded path. Two cases
	// are not failures:
	//
	//	a working copy is already there — the code arrived earlier (a previous
	//	  restore whose root_path write failed, or an import under this
	//	  runtime's layout) and only the record is stale. Adopt it; cloning
	//	  again would refuse anyway, and deleting it to "start clean" would
	//	  throw away work.
	//	anything else that exists — refuse and say what is in the way. Nothing
	//	  is moved, emptied or deleted, here or anywhere else in this flow.
	if _, statErr := os.Stat(dest); statErr == nil {
		if !s.git.HasGit(dest) {
			return domain.Repository{}, fmt.Errorf(
				"a folder already exists at %s but is not a git repository. Nothing was changed or deleted — move or remove it yourself, then try again", dest)
		}
		// Adoption is only safe if what is there is THIS repository. The two
		// benign reasons above are both reasons the same code would be sitting
		// at dest; a checkout of something else is neither, and re-pointing
		// root_path at it would hand this repository's board, index and pushes
		// a tree that belongs to another project.
		if err := s.assertSameRepo(ctx, dest, repo.RemoteURL); err != nil {
			return domain.Repository{}, err
		}
		updated, err := s.repos.UpdateRootPath(ctx, repositoryID, dest)
		if err != nil {
			return domain.Repository{}, fmt.Errorf("save restored path: %w", err)
		}
		updated.ProjectIDs = repo.ProjectIDs
		s.recordRestore(repositoryID, &domain.RepositoryRestore{
			Status: domain.RepositoryRestoreCompleted, RootPath: dest,
			StartedAt: time.Now(), FinishedAt: ptrNow(),
		})
		s.startIndex(ctx, repositoryID, dest)
		return s.withGitWarning(updated), nil
	}

	s.recordRestore(repositoryID, &domain.RepositoryRestore{
		Status: domain.RepositoryRestoreRunning, RootPath: dest, StartedAt: time.Now(),
	})
	cloneURL := repo.RemoteURL
	// The request's tenant travels with the background half. Its deadline does
	// not: a clone is minutes long and the HTTP response has already gone. What
	// runs down there still writes to this tenant's rows (UpdateRootPath, and
	// the index pass it starts), and those writes are scoped by the identity on
	// the context — from a bare context.Background() they fail with
	// tenant.ErrNoTenant, so the code would land on disk and the record would
	// never be re-pointed at it.
	restoreCtx := context.WithoutCancel(ctx)
	s.launchRestore(func() { s.runRestore(restoreCtx, repositoryID, cloneURL, dest) })
	return s.withGitWarning(repo), nil
}

// runRestore is the slow half: clone, then record where the code landed.
//
// The clone itself is the same call the GitHub import makes
// (port.GitClient.CloneRepo), which is what makes private repositories work —
// the adapter injects the stored GitHub token as an Authorization header. There
// is deliberately no second authentication path here.
func (s *Service) runRestore(parent context.Context, repositoryID uuid.UUID, cloneURL, dest string) {
	ctx, cancel := context.WithTimeout(parent, restoreCloneTimeout)
	defer cancel()

	if err := s.git.CloneRepo(ctx, cloneURL, dest); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("dest", dest).
			Msg("restore: clone failed")
		s.finishRestore(repositoryID, dest, err)
		return
	}
	// The clone is on disk but nothing points at it yet, so a failure here is
	// worth its own sentence: retrying is safe (the destination now holds a
	// working copy and the next attempt adopts it) and the user should know the
	// code did arrive.
	if _, err := s.repos.UpdateRootPath(ctx, repositoryID, dest); err != nil {
		log.Error().Err(err).Str("repository_id", repositoryID.String()).Str("dest", dest).
			Msg("restore: clone succeeded but root_path could not be updated")
		s.finishRestore(repositoryID, dest, fmt.Errorf("the code was cloned to %s but the project could not be re-pointed at it: %w", dest, err))
		return
	}
	s.finishRestore(repositoryID, dest, nil)
	log.Info().Str("repository_id", repositoryID.String()).Str("dest", dest).
		Msg("restore: working copy is back on this host")
	// The index rows survive a lost working copy (they hang off the repository
	// id), but they were built from a tree that is now freshly checked out, so
	// the pass is started for the same reason import starts one.
	s.startIndex(parent, repositoryID, dest)
}

// restoreDestination is where THIS runtime would put this repository if it were
// being imported today — not where the record says it once lived. A row written
// by the cloud pod names a directory under the pod's PVC; putting the clone
// there is either impossible (a read-only root filesystem) or wrong.
func (s *Service) restoreDestination(ctx context.Context, repo domain.Repository) (string, error) {
	if strings.TrimSpace(s.workspaceRoot) == "" {
		return "", fmt.Errorf("this runtime has no workspace root configured, so there is nowhere to put the working copy")
	}
	name := restoreDirName(repo)
	if name == "" {
		return "", fmt.Errorf("could not work out a folder name for this project")
	}
	return s.workspaceRepoPath(ctx, name)
}

// workspaceRepoPath is the one definition of this runtime's repository layout,
// shared with the GitHub import so a restored checkout and a fresh one can
// never drift apart.
//
// The tenant segment is the whole point. The name here is the REPOSITORY's
// directory name, and migration 114 re-cut repositories' unique key to
// (tenant_id, root_path) — so two customers with a repository called "api"
// became two rows legally naming one directory, and every adoption path below
// would have handed the second one the first one's checkout.
func (s *Service) workspaceRepoPath(ctx context.Context, name string) (string, error) {
	return workspace.TenantRepoDir(ctx, s.workspaceRoot, name)
}

// restoreDirName picks the directory name for the restored checkout.
//
// The recorded root path's last segment comes first: it is the name this
// repository is already known by on every host (it is what the store's
// cross-host lookup matches on), so keeping it is what stops a restore from
// registering as a different repository later. The remote is the fallback, and
// the display name the last resort.
func restoreDirName(repo domain.Repository) string {
	if name := workspace.CleanDirName(filepath.Base(filepath.Clean(strings.TrimSpace(repo.RootPath)))); name != "" {
		return name
	}
	if name := workspace.CleanDirName(repoNameFromRemote(repo.RemoteURL)); name != "" {
		return name
	}
	return workspace.CleanDirName(repo.Name)
}

// repoNameFromRemote reads the repository name out of an origin URL, covering
// both the https and the scp-style ssh forms.
func repoNameFromRemote(remoteURL string) string {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(strings.TrimRight(trimmed, "/"), ".git")
	if idx := strings.LastIndexAny(trimmed, "/:"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	return trimmed
}

// launchRestore runs the slow half. The indirection is a test seam (same shape
// as pushRunFn): tests substitute a synchronous runner so a clone's outcome can
// be asserted without waiting on a goroutine.
func (s *Service) launchRestore(fn func()) {
	if s.restoreRun != nil {
		s.restoreRun(fn)
		return
	}
	go fn()
}

// IsRestoreRunning reports whether a clone is in flight for this repository.
//
// It is also the single-flight any future automatic caller would need: making
// restore implicit (say, on dispatch to a repository whose folder is gone)
// would be safe only if it were bounded to that one repository, never fanned
// out over a list, and visible while it ran — which is exactly what this ledger
// plus Repository.GitRestore provide. It is deliberately not wired that way
// today; the user asks for the clone, and the user can see it happening.
//
// # It stays per-process, and that is not an oversight
//
// Every other in-memory ledger in this codebase became a database claim so that
// N replicas could not take the same item. This one must not, because the
// resource it guards is not shared: it is a directory on THIS host's disk,
// under this runtime's own workspace root (restoreDestination). Two replicas
// each need their own checkout — the working copy is a per-host cache of the
// git remote, which is why restoreDestination refuses to use the path the row
// records — so a claim that stopped the second replica from cloning would deny
// it the very thing it asked for.
//
// What IS shared is repositories.root_path, and two concurrent restores do race
// that write: it is a plain UPDATE and the last one wins. That race is benign
// by construction rather than by luck. The column has already stopped being
// "where the code is" and become "where some host once put it": every read goes
// through RepositoryStore.localizeRootPath, which re-anchors a foreign path
// onto the reading host's own root. So the loser of the race reads its own
// checkout back, and the winner reads its own — which is the same answer both
// would have got if the race had not happened.
func (s *Service) IsRestoreRunning(repositoryID uuid.UUID) bool {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	state := s.restores[repositoryID]
	return state != nil && state.Status == domain.RepositoryRestoreRunning
}

func (s *Service) recordRestore(repositoryID uuid.UUID, state *domain.RepositoryRestore) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	if s.restores == nil {
		s.restores = make(map[uuid.UUID]*domain.RepositoryRestore)
	}
	s.restores[repositoryID] = state
}

// finishRestore closes out an attempt, keeping git's own words on failure.
func (s *Service) finishRestore(repositoryID uuid.UUID, dest string, err error) {
	state := &domain.RepositoryRestore{
		Status: domain.RepositoryRestoreCompleted, RootPath: dest, FinishedAt: ptrNow(),
	}
	s.restoreMu.Lock()
	if previous := s.restores[repositoryID]; previous != nil {
		state.StartedAt = previous.StartedAt
	}
	s.restoreMu.Unlock()
	if state.StartedAt.IsZero() {
		state.StartedAt = time.Now()
	}
	if err != nil {
		state.Status = domain.RepositoryRestoreFailed
		state.Error = err.Error()
	}
	s.recordRestore(repositoryID, state)
}

// restoreState is the copy handed to callers, so a reader can never mutate the
// ledger it is reading.
func (s *Service) restoreState(repositoryID uuid.UUID) *domain.RepositoryRestore {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	state, ok := s.restores[repositoryID]
	if !ok || state == nil {
		return nil
	}
	copied := *state
	return &copied
}

func ptrNow() *time.Time {
	now := time.Now()
	return &now
}
