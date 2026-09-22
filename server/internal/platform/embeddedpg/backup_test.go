package embeddedpg

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCluster(t *testing.T, pgData string) {
	t.Helper()
	for path, body := range map[string]string{
		"PG_VERSION":        "17\n",
		"base/1/1259":       "heap",
		"global/pg_control": "control",
	} {
		full := filepath.Join(pgData, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("base", filepath.Join(pgData, "pg_link")); err != nil {
		t.Fatal(err)
	}
}

func TestBackupCopiesAnExistingClusterOnce(t *testing.T) {
	dataDir := t.TempDir()
	pgData := filepath.Join(dataDir, "postgres")
	backup := filepath.Join(dataDir, preDropTenancyBackup)
	writeCluster(t, pgData)

	copied, err := backupClusterOnce(pgData, backup)
	if err != nil || !copied {
		t.Fatalf("first backup: copied=%v err=%v", copied, err)
	}
	got, err := os.ReadFile(filepath.Join(backup, "base/1/1259"))
	if err != nil || string(got) != "heap" {
		t.Fatalf("copied file = %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(backup, "global/pg_control"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("copied mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if link, err := os.Readlink(filepath.Join(backup, "pg_link")); err != nil || link != "base" {
		t.Fatalf("symlink = %q, %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(backup, backupCompleteMarker)); err != nil {
		t.Fatalf("no completion marker: %v", err)
	}
	if _, err := os.Stat(backup + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial copy left behind: %v", err)
	}

	// A second start must keep the pre-133 copy, not overwrite it with the
	// migrated cluster.
	if err := os.WriteFile(filepath.Join(pgData, "base/1/1259"), []byte("migrated"), 0o600); err != nil {
		t.Fatal(err)
	}
	copied, err = backupClusterOnce(pgData, backup)
	if err != nil || copied {
		t.Fatalf("second backup: copied=%v err=%v", copied, err)
	}
	if got, _ := os.ReadFile(filepath.Join(backup, "base/1/1259")); string(got) != "heap" {
		t.Fatalf("backup was overwritten: %q", got)
	}
}

func TestBackupRedoesACopyThatNeverFinished(t *testing.T) {
	dataDir := t.TempDir()
	pgData := filepath.Join(dataDir, "postgres")
	backup := filepath.Join(dataDir, preDropTenancyBackup)
	writeCluster(t, pgData)
	if err := os.MkdirAll(filepath.Join(backup, "base"), 0o700); err != nil {
		t.Fatal(err)
	}

	copied, err := backupClusterOnce(pgData, backup)
	if err != nil || !copied {
		t.Fatalf("copied=%v err=%v", copied, err)
	}
	if _, err := os.Stat(filepath.Join(backup, "PG_VERSION")); err != nil {
		t.Fatalf("unfinished copy was not redone: %v", err)
	}
}

func TestBackupSkipsAFreshDataDirectory(t *testing.T) {
	dataDir := t.TempDir()
	pgData := filepath.Join(dataDir, "postgres")
	if err := os.MkdirAll(pgData, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dataDir, preDropTenancyBackup)

	copied, err := backupClusterOnce(pgData, backup)
	if err != nil || copied {
		t.Fatalf("copied=%v err=%v", copied, err)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("a fresh data directory gained %d entries, want only postgres", len(entries)-1)
	}
}

func TestBackupSkipsAClusterCreatedAfter133(t *testing.T) {
	dataDir := t.TempDir()
	pgData := filepath.Join(dataDir, "postgres")
	backup := filepath.Join(dataDir, preDropTenancyBackup)
	writeCluster(t, pgData)
	if err := markBackupNotNeeded(backup); err != nil {
		t.Fatal(err)
	}

	copied, err := backupClusterOnce(pgData, backup)
	if err != nil || copied {
		t.Fatalf("copied=%v err=%v", copied, err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup directory created: %v", err)
	}
}

func TestBackupFailureIsReturnedAndLeavesNoMarker(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	dataDir := t.TempDir()
	pgData := filepath.Join(dataDir, "postgres")
	backup := filepath.Join(dataDir, preDropTenancyBackup)
	writeCluster(t, pgData)
	unreadable := filepath.Join(pgData, "global/pg_control")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

	copied, err := backupClusterOnce(pgData, backup)
	if err == nil || copied {
		t.Fatalf("copied=%v err=%v; want an error", copied, err)
	}
	for _, path := range []string{backup, backup + ".partial"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s left behind after a failed copy: %v", path, err)
		}
	}
}
