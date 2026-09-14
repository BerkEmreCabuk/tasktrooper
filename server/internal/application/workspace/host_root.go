package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// HostRootPath re-anchors a stored repository root path onto THIS host, inside
// the subtree that belongs to the tenant on ctx.
//
// `repositories.root_path` is an absolute path belonging to whichever host
// wrote the row, and that used to be the same host forever. It no longer is:
// one tenant database can now be served both by the cloud pod (PVC mounted at
// /data) and by the user's own Mac running the same binary behind a reverse
// tunnel, where DATA_DIR is <repo>/local-runner/data. A board run on the Mac
// read the cloud-written "/data/workspaces/repos/<name>", handed it to
// git clone, and died on `mkdir /data: read-only file system` — the macOS root
// filesystem cannot be written, so no amount of retrying would have helped.
//
// The translation is deliberately symmetric: the pod re-anchors a Mac-written
// path exactly the same way, so a write from either host stays harmless to the
// other and neither host has to be declared the owner of the column.
//
// The rule, in order:
//
//  1. A stored path that is usable here is used unchanged (see UsableHostPath).
//     Existence comes first on purpose — a self-hosted user may legitimately
//     point a repository at a checkout far outside the workspace root, and
//     re-anchoring that would move a working repo for no reason.
//  2. Otherwise the path is foreign, and only its final segment carries
//     meaning here. That segment is placed inside the CALLING TENANT's subtree
//     (see reanchorRel for where within it), never under the shared workspace
//     root: for a repository the segment is a NAME, names like "api" and "web"
//     collide across customers by default, and a re-anchor into the shared root
//     landed two customers on one working copy — which is the one outcome the
//     tenant segment exists to make impossible.
//
// A context with no identity gets no re-anchor at all. There is no destination
// that could be right, and the caller keeps an error naming the real path.
//
// The second return value reports whether a re-anchor happened, so the caller
// can log the translation once rather than silently swapping paths.
func HostRootPath(ctx context.Context, stored, workspaceRoot string, allowedRoots []string) (string, bool) {
	trimmed := strings.TrimSpace(stored)
	if trimmed == "" {
		return stored, false
	}
	if UsableHostPath(ctx, trimmed, workspaceRoot, allowedRoots) {
		return stored, false
	}
	// Without a workspace root there is nothing to re-anchor onto, and
	// inventing one (a relative default resolved against the process's working
	// directory) would be worse than leaving the stored path alone: the caller
	// still gets the original error naming the real path.
	if strings.TrimSpace(workspaceRoot) == "" {
		return stored, false
	}
	tenantRoot, err := TenantRoot(ctx, workspaceRoot)
	if err != nil {
		return stored, false
	}
	name := CleanDirName(filepath.Base(filepath.Clean(trimmed)))
	if name == "" {
		return stored, false
	}
	reanchored := filepath.Join(tenantRoot, reanchorRel(name))
	if reanchored == trimmed {
		return stored, false
	}
	return reanchored, true
}

// reanchorRel places a foreign path's final segment where THIS host would have
// put it inside the tenant's subtree.
//
// A task checkout and a chat's own scratch directory are direct children of the
// root — task-<uuid> and <session-uuid> respectively — and anything else is a
// repository's directory name, which belongs under repos/. Getting this wrong
// is not cosmetic: the re-anchored path is what the index mirror clones into
// and what the board runner checks out, so it has to be the same path the code
// that CREATES these directories would derive, or one repository ends up with
// two working copies that never converge.
func reanchorRel(name string) string {
	if _, err := uuid.Parse(strings.TrimPrefix(name, "task-")); err == nil {
		return name
	}
	return filepath.Join(reposDirName, name)
}

// UsableHostPath reports whether path can be used as-is on this host.
//
// Two things make a path usable, and the second is not redundant: a path that
// exists is obviously this host's, and a path that does not exist yet but lies
// under one of this tenant's roots is one this host may still create — which is
// exactly the state a fresh pod is in before the working copy is restored from
// remote_url. Treating a not-yet-cloned local path as foreign would re-anchor
// it on every boot and hide the real "clone me" case.
//
// The tenant's own subtree is the root that is always allowed, not the shared
// workspace root: a path under a DIFFERENT tenant's subtree that has not been
// created yet must read as foreign, or the not-yet-cloned case would quietly
// hand one customer a destination inside another's.
//
// allowedRoots are the operator's extra roots (config `indexer.allowed_roots`),
// which exist so a self-hosted user can point repositories at checkouts outside
// the managed workspace.
func UsableHostPath(ctx context.Context, path, workspaceRoot string, allowedRoots []string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return false
	}
	if _, statErr := os.Stat(abs); statErr == nil {
		return true
	}
	roots := make([]string, 0, len(allowedRoots)+1)
	if tenantRoot, err := TenantRoot(ctx, workspaceRoot); err == nil {
		roots = append(roots, tenantRoot)
	}
	roots = append(roots, allowedRoots...)
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if ok, err := IsWithinRoot(abs, root); err == nil && ok {
			return true
		}
	}
	return false
}
