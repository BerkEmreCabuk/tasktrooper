package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// The one tenant directory a local install ever wrote. Spelled out rather than
// imported so this migration does not depend on the tenant package.
const (
	legacyTenantsDirName = "tenants"
	legacyLocalTenantID  = "00000000-0000-0000-0000-000000000001"
)

// legacyMergeableDirs are containers whose children are independent units (one
// clone, one agent, one CLI flavor's catalog), so a copy on each side can be
// combined entry by entry. A task checkout or a session directory is a single
// unit: merging two of those would produce a tree neither side wrote, so they
// are only ever moved whole or left in place.
var legacyMergeableDirs = map[string]bool{
	reposDirName: true,
	"agents":     true,
	"agent-cli":  true,
}

// StoredPathRewriter re-points the absolute workspace paths the database keeps.
//
// RewriteStoredPaths visits every distinct stored value that contains one of
// markers, in one transaction, and replaces it wherever rewrite returns true.
// It reports how many rows changed.
type StoredPathRewriter interface {
	RewriteStoredPaths(ctx context.Context, markers []string, rewrite func(stored string) (string, bool)) (int, error)
}

// FlattenResult counts what one FlattenLegacyLayout pass did.
type FlattenResult struct {
	// Moved is the number of directory entries renamed into the flat layout.
	Moved int
	// Conflicts is the number of entries left in place because the flat
	// destination was already taken.
	Conflicts int
	// Rewritten is the number of database rows re-pointed at the flat layout.
	Rewritten int
}

// FlattenLegacyLayout moves an install written under <root>/tenants/<local id>/
// to the flat layout (<root>/task-<uuid>, <root>/repos/<name>, ...) and
// re-points the stored paths that named the old location.
//
// It runs at boot, before anything reads a workspace, and is idempotent: once
// the move has happened there is nothing left to move and no row still names
// the old layout. Nothing is ever deleted or overwritten. A taken destination
// leaves the entry where it is with a warning, and only directories that end
// up empty are removed. One entry that cannot be moved is logged and skipped;
// the error return is only for the database step, and the caller logs it
// rather than failing startup, because every stored path is still re-anchored
// on read.
func FlattenLegacyLayout(ctx context.Context, configuredRoot string, paths StoredPathRewriter) (FlattenResult, error) {
	var res FlattenResult
	if strings.TrimSpace(configuredRoot) == "" {
		return res, nil
	}
	root, err := ResolveRoot(configuredRoot)
	if err != nil {
		return res, err
	}
	tenantsDir := filepath.Join(root, legacyTenantsDirName)
	legacy := filepath.Join(tenantsDir, legacyLocalTenantID)

	moveLegacyEntries(legacy, root, &res)
	removeIfEmptyDir(legacy)
	logLeftoverTenantDirs(tenantsDir)
	removeIfEmptyDir(tenantsDir)

	if paths != nil {
		n, err := paths.RewriteStoredPaths(ctx, legacyPathMarkers(), flattenStoredPath)
		res.Rewritten = n
		if err != nil {
			return res, fmt.Errorf("rewrite stored workspace paths: %w", err)
		}
	}
	if res.Moved > 0 || res.Conflicts > 0 || res.Rewritten > 0 {
		log.Info().
			Str("root", root).
			Int("moved", res.Moved).
			Int("conflicts", res.Conflicts).
			Int("rows_rewritten", res.Rewritten).
			Msg("workspace layout: flattened the per-tenant workspace directory")
	}
	return res, nil
}

// moveLegacyEntries moves each child of from into to.
func moveLegacyEntries(from, to string, res *FlattenResult) {
	entries, err := os.ReadDir(from)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Warn().Err(err).Str("dir", from).Msg("workspace layout: could not read the per-tenant directory; nothing was moved")
		}
		return
	}
	for _, entry := range entries {
		src := filepath.Join(from, entry.Name())
		dst := filepath.Join(to, entry.Name())
		switch {
		case !pathTaken(dst):
			moveEntry(src, dst, res)
		case legacyMergeableDirs[entry.Name()] && isRealDir(src) && isRealDir(dst):
			moveChildren(src, dst, res)
			removeIfEmptyDir(src)
		default:
			leaveConflict(src, dst, res)
		}
	}
}

// moveChildren merges one level down: each child moves unless its name is
// already taken on the destination side.
func moveChildren(from, to string, res *FlattenResult) {
	entries, err := os.ReadDir(from)
	if err != nil {
		log.Warn().Err(err).Str("dir", from).Msg("workspace layout: could not read a directory to merge; it was left in place")
		return
	}
	for _, entry := range entries {
		src := filepath.Join(from, entry.Name())
		dst := filepath.Join(to, entry.Name())
		if pathTaken(dst) {
			leaveConflict(src, dst, res)
			continue
		}
		moveEntry(src, dst, res)
	}
}

func moveEntry(src, dst string, res *FlattenResult) {
	if err := os.Rename(src, dst); err != nil {
		log.Warn().Err(err).Str("from", src).Str("to", dst).Msg("workspace layout: could not move an entry; it was left where it is")
		return
	}
	res.Moved++
}

func leaveConflict(src, dst string, res *FlattenResult) {
	res.Conflicts++
	log.Warn().Str("from", src).Str("to", dst).Msg("workspace layout: the flat destination already exists; the old entry was left where it is")
}

// logLeftoverTenantDirs names whatever is still under tenants/: another
// tenant's directory, or the local one's conflicts. None of it is touched.
func logLeftoverTenantDirs(tenantsDir string) {
	entries, err := os.ReadDir(tenantsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		log.Warn().Str("path", filepath.Join(tenantsDir, entry.Name())).Msg("workspace layout: left in place under tenants/")
	}
}

// removeIfEmptyDir removes dir only when it is a directory with nothing in it.
// os.Remove refuses a non-empty directory, so no content can go with it.
func removeIfEmptyDir(dir string) {
	if !isRealDir(dir) {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	if err := os.Remove(dir); err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("workspace layout: could not remove an empty directory")
	}
}

// pathTaken treats anything but a definite "does not exist" as taken, so an
// unreadable destination is a conflict rather than a rename target.
func pathTaken(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// isRealDir is a directory that is not a symlink: merging through a link would
// move entries into wherever it points.
func isRealDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// legacyPathMarkers match the tenant segment under both separators: a Windows
// build stored backslash paths into the same columns.
func legacyPathMarkers() []string {
	return []string{
		"/" + legacyTenantsDirName + "/" + legacyLocalTenantID,
		`\` + legacyTenantsDirName + `\` + legacyLocalTenantID,
	}
}

// flattenStoredPath maps a stored path under the old layout to the flat one.
//
// A stored path that still exists is kept: its entry was not moved (a
// conflict, a failed rename), and re-pointing the row would hand it whatever
// blocked the move. Otherwise either the flat path exists (the move happened)
// or neither does (another host's path, or a directory already gone), and in
// both cases the flat path is what this layout derives today.
func flattenStoredPath(stored string) (string, bool) {
	flat, ok := stripLegacySegment(stored)
	if !ok || pathTaken(stored) {
		return "", false
	}
	return flat, true
}

// stripLegacySegment removes a whole "/tenants/<local id>" component, under
// either separator, and nothing that merely starts with it.
func stripLegacySegment(stored string) (string, bool) {
	for _, marker := range legacyPathMarkers() {
		i := strings.Index(stored, marker)
		if i < 0 {
			continue
		}
		rest := stored[i+len(marker):]
		if rest != "" && rest[0] != '/' && rest[0] != '\\' {
			continue
		}
		return stored[:i] + rest, true
	}
	return "", false
}
