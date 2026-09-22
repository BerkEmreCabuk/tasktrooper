//go:build windows

package database

// stopWhenOrphaned is a no-op on Windows: the unix side's detached /bin/sh
// watcher (kill -0/-INT, Setsid) has no equivalent here, so a hard-killed
// process leaves its embedded cluster running. The clean-shutdown Stop() path is
// unaffected.
func stopWhenOrphaned(dataDir string) {}
