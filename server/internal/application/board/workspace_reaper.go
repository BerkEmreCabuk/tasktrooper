package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// WorkspaceReaperInterval is how often finished tasks' checkouts are collected.
//
// Nothing waits on this, so it is paced by what it costs rather than by how
// fresh it has to be: a pass reads one directory listing and one task list.
// Hourly keeps a burst of releases from sitting on the volume for a day while
// costing far less than the disk it reclaims.
const WorkspaceReaperInterval = time.Hour

// WorkspaceReapGrace is how long a finished task keeps its checkout.
//
// It is not a safety margin against the reaper being wrong — it is the window
// in which a human realises a release was premature and drags the card back.
// Nothing prevents that: the lifecycle gate guards entry into done/released and
// not the exit, and the transition whitelist is unseeded, so released →
// in_progress is an ordinary move. A resumed task re-clones, because the
// workspace is prepared before any agent runs, so reaping too early costs
// uncommitted work rather than a broken task — and uncommitted work is exactly
// what a same-day reopen would be reaching for.
const WorkspaceReapGrace = 48 * time.Hour

// TaskLister is every task the tenant has, across repositories. The reaper
// needs the whole set rather than a lookup per directory: it has to tell a task
// in a live column from a task that no longer exists at all, and a per-ID
// lookup cannot distinguish "deleted" from "could not be read".
type TaskLister interface {
	ListAll(ctx context.Context) ([]domain.BoardTask, error)
}

// ActiveTaskProbe reports whether ANY replica is executing the task right now.
//
// It used to ask the local board runner, which answers for one process's map.
// That was a correct question when one process served a tenant and is a
// dangerous one now: a checkout being written by a run on replica A looks idle
// to replica B, whose reaper would delete the directory out from under it
// mid-commit. It is satisfied by the run store instead, where "executing"
// means a 'running' row with a heartbeat inside liveWithin — the one answer
// every process can see.
type ActiveTaskProbe interface {
	HasLiveRunForTask(ctx context.Context, taskID uuid.UUID, liveWithin time.Duration) (bool, error)
}

// WorkspaceReaper deletes per-task checkouts under the workspace root once the
// task they belong to is finished or gone.
//
// It exists because a task workspace is a full repository clone plus whatever
// the build wrote into it — a few hundred megabytes each — and nothing ever
// removed one. The volume is shared and the failure is silent until it is
// total: builds start failing with ENOSPC in tasks that have nothing to do with
// the ones holding the space, and the agent reports a broken dev server rather
// than a full disk.
//
// The sweep is driven from the directory listing rather than from the task
// list, because the directories that matter most are the ones with no task left
// to enumerate. Every decision is per directory and none is bulk: an unreadable
// task list means "reap nothing this pass", never "reap everything".
//
// ── Why the listing is rooted at the tenant subtree ─────────────────────────
//
// root is the SHARED workspace root and this sweep runs once per tenant, so
// listing root itself enumerated every customer's directories while the keep
// set — tasks.ListAll — is filtered by row-level security to one customer's
// rows. The difference between the two was deleted. Both guards written to stop
// exactly that were neutralised by the same RLS that created it: the live-run
// probe runs on the tenant-scoped pool, so another tenant's running task came
// back as a confident (false, nil) rather than as an error, and the
// "every uncertain answer is keep" rule never fired because nothing was
// uncertain; and the done/released column check is only reached for a task that
// IS in the keep set, which another tenant's never is.
//
// Neither guard is repairable from inside this file, because neither is allowed
// to see the row it would need. So the listing is rooted at
// workspace.TenantRoot instead: a directory belonging to a tenant this sweep is
// not scoped to is not enumerated, and therefore cannot reach a delete branch
// at all. A pass that cannot work out whose subtree it is sweeping deletes
// nothing.
type WorkspaceReaper struct {
	tasks  TaskLister
	active ActiveTaskProbe
	root   string
	grace  time.Duration
}

func NewWorkspaceReaper(tasks TaskLister, active ActiveTaskProbe, root string, grace time.Duration) *WorkspaceReaper {
	if grace <= 0 {
		grace = WorkspaceReapGrace
	}
	return &WorkspaceReaper{tasks: tasks, active: active, root: strings.TrimSpace(root), grace: grace}
}

// Start sweeps on interval until ctx ends, beginning with one pass at startup.
//
// Unlike the device sweeper this is safe to run immediately: it claims nothing
// and races nothing, and a pod that just restarted because its volume filled is
// exactly when the first pass is worth the most.
func (r *WorkspaceReaper) Start(ctx context.Context, interval time.Duration) {
	if r == nil || r.tasks == nil || r.root == "" {
		return
	}
	if interval <= 0 {
		interval = WorkspaceReaperInterval
	}
	go func() {
		r.Sweep(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.Sweep(ctx)
			}
		}
	}()
}

// Sweep reclaims every eligible directory in one pass, for the tenant on ctx.
// Separate from Start, and exported, so it can be driven without a clock — the
// property it has to hold (another tenant's directories are untouched) is only
// testable against a real database, from another package.
func (r *WorkspaceReaper) Sweep(ctx context.Context) {
	// Whose directories this pass may even look at. Start's context carries the
	// local identity, so failing here means a caller built a context by hand
	// — and the answer to "I cannot tell whose these are" is to touch nothing,
	// not to fall back to the shared root.
	root, err := workspace.TenantRoot(ctx, r.root)
	if err != nil {
		log.Warn().Err(err).Msg("workspace reaper: no tenant on the sweep context, so nothing was reaped")
		return
	}
	all, err := r.tasks.ListAll(ctx)
	if err != nil {
		// Without the task list every directory looks orphaned. Acting on that
		// reading would delete the workspace of every running task the moment
		// the database hiccups, so a failed list ends the pass instead.
		log.Warn().Err(err).Msg("workspace reaper: listing tasks failed, skipping this pass")
		return
	}
	byID := make(map[uuid.UUID]domain.BoardTask, len(all))
	for _, t := range all {
		byID[t.ID] = t
	}

	r.adoptFlatTaskDirs(ctx, root, byID)

	entries, err := os.ReadDir(root)
	if err != nil {
		// A missing root is normal before the first task ever runs.
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Str("root", root).Msg("workspace reaper: reading the workspace root failed")
		}
		return
	}

	var reaped int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID, ok := parseTaskDirName(entry.Name())
		if !ok {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if !r.eligible(ctx, taskID, byID, path) {
			continue
		}
		// Rooted at the tenant subtree, not at r.root: the confinement check
		// has to enforce the same boundary the listing was taken from, or a
		// symlinked entry would be confined to the shared volume rather than to
		// this tenant's part of it.
		if err := workspace.RemoveDirWithin(root, path); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Msg("workspace reaper: removing a finished task's workspace failed")
			continue
		}
		reaped++
		log.Info().Str("task_id", taskID.String()).Msg("workspace reaper: reclaimed a finished task's workspace")
	}
	if reaped > 0 {
		log.Info().Int("count", reaped).Msg("workspace reaper: pass complete")
	}
}

// adoptFlatTaskDirs moves task checkouts left at the shared workspace root by
// the layout that predates the tenant segment into the subtree of the tenant
// that owns them.
//
// It exists because those directories hold UNCOMMITTED work — the one thing in
// this layout that is not re-clonable — and a fresh layout on its own would
// strand it: nothing derives the flat path any more, so the work would sit
// there unreachable until an operator noticed the disk.
//
// Ownership is PROVEN, never guessed: a directory is moved only when its task
// uuid appears in this tenant's own row-level-security-filtered task list, and
// task uuids cannot collide across tenants. Anything that cannot be proven —
// another tenant's task, a task deleted before this ran, repos/, agents/,
// lost+found — is left exactly where it is. This function never deletes and
// never overwrites: a destination that already exists means the tenant-scoped
// copy is the current one, and the flat leftover is not it.
//
// It is a no-op on every deployment that has only ever written the current
// layout, which is every new one.
func (r *WorkspaceReaper) adoptFlatTaskDirs(ctx context.Context, tenantRoot string, byID map[uuid.UUID]domain.BoardTask) {
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID, ok := parseTaskDirName(entry.Name())
		if !ok {
			continue
		}
		if _, mine := byID[taskID]; !mine {
			continue
		}
		// Nothing derives the flat path any more, so a run cannot be writing
		// into one — unless it started before this build was deployed. Moving a
		// directory out from under a live run is the failure the reaper's own
		// live-run probe exists to prevent, and it applies to a rename as much
		// as to a delete. A probe that cannot answer is a skip.
		if r.active != nil {
			live, err := r.active.HasLiveRunForTask(ctx, taskID, runLiveWithin)
			if err != nil || live {
				continue
			}
		}
		from := filepath.Join(r.root, entry.Name())
		to := filepath.Join(tenantRoot, entry.Name())
		if _, statErr := os.Stat(to); statErr == nil {
			continue
		}
		if err := os.MkdirAll(tenantRoot, 0o755); err != nil {
			log.Warn().Err(err).Str("root", tenantRoot).Msg("workspace reaper: could not create the tenant workspace root")
			return
		}
		if err := os.Rename(from, to); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Str("from", from).Str("to", to).
				Msg("workspace reaper: could not move a task workspace into the tenant subtree; it was left where it is")
			continue
		}
		log.Info().Str("task_id", taskID.String()).Str("from", from).Str("to", to).
			Msg("workspace reaper: moved a task workspace from the shared root into the tenant subtree")
	}
}

// eligible decides a single directory. Every uncertain answer is "keep".
//
// Its guards are only ever asked about a directory inside the sweeping tenant's
// own subtree — see the type comment for why that has to be established by the
// listing rather than here. Within that subtree the live-run probe and the
// column check mean what they were written to mean, because the rows they read
// are rows this tenant can see.
func (r *WorkspaceReaper) eligible(ctx context.Context, taskID uuid.UUID, byID map[uuid.UUID]domain.BoardTask, path string) bool {
	// A task ANY replica is executing owns its checkout whatever the row says.
	// A move to done mid-run must not pull the directory out from under the run
	// that is about to commit into it — and on a shared deployment that run is
	// very often not here.
	//
	// A failed probe is "keep", like every other uncertain answer in this
	// function: reclaiming disk is never urgent enough to risk deleting a live
	// checkout because one query did not answer.
	if r.active != nil {
		live, err := r.active.HasLiveRunForTask(ctx, taskID, runLiveWithin)
		if err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).
				Msg("workspace reaper: could not tell whether a run holds this workspace, keeping it")
			return false
		}
		if live {
			return false
		}
	}

	task, known := byID[taskID]
	if !known {
		// No row anywhere: the task was deleted before deletion also removed the
		// directory, or through a path that bypassed it. Age it off the
		// directory's own mtime, the only clock left once the row is gone.
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		return time.Since(info.ModTime()) > r.grace
	}

	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		return false
	}
	// UpdatedAt is stamped by the column move itself, so this measures time
	// spent finished rather than time since the task was created.
	return time.Since(task.UpdatedAt) > r.grace
}

// parseTaskDirName recognises the "task-{uuid}" layout workspace.TaskDir
// writes, and nothing else. Anything unparseable — the repos/ cache, agents/,
// the volume's lost+found — is left alone.
func parseTaskDirName(name string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(name, "task-")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
