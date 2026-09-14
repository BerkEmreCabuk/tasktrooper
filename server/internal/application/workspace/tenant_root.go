package workspace

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// tenantsDirName is the segment that separates one customer's files from
// another's under the shared workspace root.
//
// A named segment rather than the bare uuid: session workspaces are
// <root>/<session-uuid>, so a tenant directory one level down would be
// indistinguishable from a session's, and anything walking the root would have
// to guess which kind of uuid it was looking at.
const tenantsDirName = "tenants"

// reposDirName holds one directory per repository this tenant has a mirror
// clone of. It is the only path component under a tenant root that is not a
// uuid, which is why the tenant segment above it is what keeps two customers'
// repositories called "api" apart.
const reposDirName = "repos"

// TenantRoot is the subtree of the shared workspace root that belongs to the
// tenant on ctx: <root>/tenants/<tenant-uuid>.
//
// One process, one PersistentVolumeClaim, every customer. <root> itself is a
// namespace nobody owns, and every path that used to be derived from it
// directly — repos/<name>, task-<uuid> — was reachable by a tenant that did
// not create it. Row-level security does not reach a filesystem path, so the
// isolation has to BE the path: deriving from here makes "this is not mine" a
// property of where the bytes are, rather than of a query that was supposed to
// filter them and, on another tenant's row, confidently returns nothing.
//
// A context with no identity is an error, never <root>. A caller that cannot
// say whose files it is asking for must not be handed the shared namespace —
// that is how the flat layout came to be shared in the first place.
func TenantRoot(ctx context.Context, configuredRoot string) (string, error) {
	absRoot, err := ResolveRoot(configuredRoot)
	if err != nil {
		return "", err
	}
	id, ok := tenant.ID(ctx)
	if !ok {
		return "", fmt.Errorf("resolve tenant workspace root: %w", tenant.ErrNoTenant)
	}
	return filepath.Join(absRoot, tenantsDirName, id.String()), nil
}

// TenantTaskDir is a board task's own checkout:
// <root>/tenants/<tenant-uuid>/task-<task-uuid>.
//
// The rule was spelled out by hand in seven places, which was survivable while
// nothing ever deleted one and one customer owned the disk. It is not now: a
// reaper that derives the path even slightly differently from the code that
// created it either misses directories forever or removes the wrong one, and
// "the wrong one" is another customer's uncommitted work.
func TenantTaskDir(ctx context.Context, configuredRoot string, taskID uuid.UUID) (string, error) {
	root, err := TenantRoot(ctx, configuredRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "task-"+taskID.String()), nil
}

// TenantReposDir is where this tenant's mirror clones live.
func TenantReposDir(ctx context.Context, configuredRoot string) (string, error) {
	root, err := TenantRoot(ctx, configuredRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, reposDirName), nil
}

// TenantRepoDir is one mirror clone: <root>/tenants/<tenant-uuid>/repos/<name>.
//
// name is the repository's directory name and must be a single path component:
// it is joined onto a root, and a value that can climb out of it would put one
// tenant's clone in another's subtree, which is the whole thing this layout
// exists to prevent.
func TenantRepoDir(ctx context.Context, configuredRoot, name string) (string, error) {
	clean := CleanDirName(name)
	if clean == "" {
		return "", fmt.Errorf("repository directory name %q is not a single path component", name)
	}
	repos, err := TenantReposDir(ctx, configuredRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(repos, clean), nil
}

// OwnedByTenant reports whether path lies inside the subtree belonging to the
// tenant on ctx.
//
// The error case is deliberately not folded into the bool: every caller of this
// is deciding whether to delete or to adopt, and "I could not work out whose
// this is" has to be distinguishable from "it is not yours" so both can be
// treated as refusals rather than one of them defaulting to a false that reads
// like an answer.
func OwnedByTenant(ctx context.Context, configuredRoot, path string) (bool, error) {
	root, err := TenantRoot(ctx, configuredRoot)
	if err != nil {
		return false, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolve path: %w", err)
	}
	return IsWithinRoot(abs, root)
}
