package embeddedpg

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// preDropTenancyBackup is where the cluster is copied before the first start
// of the build that ships migration 133, which drops every tenant_id column and
// cannot be undone. The way back is to put this copy in place of the cluster.
const preDropTenancyBackup = "postgres-backup-pre-133"

const (
	backupCompleteMarker = ".complete"
	// backupNotNeededSuffix marks a data directory whose cluster was created by
	// a build that already had migration 133, so a later start does not copy a
	// post-133 cluster under a pre-133 name.
	backupNotNeededSuffix = ".not-needed"
)

func backupSettled(pgData, backupDir string) bool {
	return fileExists(filepath.Join(backupDir, backupCompleteMarker)) ||
		fileExists(backupDir+backupNotNeededSuffix) ||
		!fileExists(filepath.Join(pgData, "PG_VERSION"))
}

// backupClusterOnce copies the STOPPED cluster at pgData to backupDir unless a
// finished copy (or the not-needed marker) is already there, and reports
// whether it copied. A data directory with no cluster yet is left alone and
// nothing is written.
//
// The copy goes to a sibling that is renamed into place, and the marker is
// written last, so an interrupted copy is never taken for a good one.
func backupClusterOnce(pgData, backupDir string) (bool, error) {
	if backupSettled(pgData, backupDir) {
		return false, nil
	}
	partial := backupDir + ".partial"
	// Both leftovers are this function's own unfinished attempts: neither
	// carries the marker.
	if err := os.RemoveAll(partial); err != nil {
		return false, err
	}
	if err := os.RemoveAll(backupDir); err != nil {
		return false, err
	}
	if err := copyTree(pgData, partial); err != nil {
		_ = os.RemoveAll(partial)
		return false, err
	}
	if err := os.Rename(partial, backupDir); err != nil {
		_ = os.RemoveAll(partial)
		return false, err
	}
	if err := os.WriteFile(filepath.Join(backupDir, backupCompleteMarker), nil, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func markBackupNotNeeded(backupDir string) error {
	return os.WriteFile(backupDir+backupNotNeededSuffix,
		[]byte("This cluster was created with migration 133 already in place; there is nothing to back up.\n"), 0o600)
}

// copyTree copies directories, regular files and symlinks with their
// permission bits. Anything else (a socket, say) is not cluster data and is
// skipped.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if err := os.Mkdir(target, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(path, target, info.Mode().Perm())
		default:
			return nil
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
