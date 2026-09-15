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

// TaskLister is every task on the board, across repositories. The reaper
// needs the whole set rather than a lookup per directory: it has to tell a task
// in a live column from a task that no longer exists at all, and a per-ID
// lookup cannot distinguish "deleted" from "could not be read".
type TaskLister interface {
	ListAll(ctx context.Context) ([]domain.BoardTask, error)
}

// ActiveTaskProbe reports whether ANY replica is executing the task right now.
//
// It used to ask the local board runner, which answers for one process's map.
// That was a correct question when one process served the board and is a
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

// Sweep reclaims every eligible directory in one pass. Separate from Start, and
// exported, so it can be driven without a clock.
func (r *WorkspaceReaper) Sweep(ctx context.Context) {
	root, err := workspace.ResolveRoot(r.root)
	if err != nil {
		log.Warn().Err(err).Msg("workspace reaper: could not resolve the workspace root, so nothing was reaped")
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

// eligible decides a single directory. Every uncertain answer is "keep".
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
