package postgres

import (
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
)

// hostRoots is the read-time path translation shared by every store whose rows
// carry an absolute filesystem path.
//
// The columns it serves (repositories.root_path, sessions.workspace_dir,
// sessions.project_root, workspace_indexes.root_path) all hold an absolute path
// belonging to whichever host wrote the row, and that used to be the same host
// forever. It no longer is: one database can be served both by the cloud pod
// (PVC mounted at /data) and by the user's own Mac behind a reverse tunnel,
// where DATA_DIR is <repo>/local-runner/data and /data cannot exist at all. A
// board run on the Mac read the pod's "/data/..." and died on
// `mkdir /data: read-only file system`.
//
// One type rather than a copy per store: the rule, the "usable here" test and
// the log-once bookkeeping are identical for every one of those columns, and a
// second copy is a second place for them to drift apart.
type hostRoots struct {
	workspaceRoot string
	allowedRoots  []string
	// logged keys the stored paths already reported, so the INFO line is
	// emitted once per foreign path instead of once per row per poll.
	logged sync.Map
}

// set records which filesystem this store is reading rows on behalf of. The
// zero value is the old pass-through: with no workspace root nothing is ever
// re-anchored and stored paths are returned verbatim.
func (h *hostRoots) set(workspaceRoot string, allowedRoots []string) {
	h.workspaceRoot = workspaceRoot
	h.allowedRoots = allowedRoots
}

// usable reports whether a stored path can be used on this host as-is.
func (h *hostRoots) usable(path string) bool {
	if h == nil {
		return false
	}
	return workspace.UsableHostPath(path, h.workspaceRoot, h.allowedRoots)
}

// localize translates one stored path onto this host.
//
// It returns the resolved path, whether a re-anchor happened at all, and
// whether this is the FIRST time this exact stored path has been re-anchored —
// so the caller can log the translation once, with the field names its own
// table deserves, rather than once per row per poll.
func (h *hostRoots) localize(stored string) (resolved string, reanchored, first bool) {
	if h == nil || stored == "" {
		return stored, false, false
	}
	resolved, reanchored = workspace.HostRootPath(stored, h.workspaceRoot, h.allowedRoots)
	if !reanchored {
		return stored, false, false
	}
	_, seen := h.logged.LoadOrStore(stored, struct{}{})
	return resolved, true, !seen
}
