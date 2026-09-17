//go:build windows

package database

// stopWhenOrphaned is a no-op on Windows: the unix side's watcher is a
// detached `/bin/sh` script using kill -0/-INT and Setsid to survive a
// SIGKILLed parent, none of which exist on this platform. A Windows process
// killed hard (Task Manager "End task", a CI runner's own force-stop) leaves
// its embedded cluster running the same way the unix side's watcher exists
// to prevent — this only accepts that gap rather than shipping an unverified
// PowerShell equivalent. The normal Stop() path (a clean shutdown) is
// unaffected either way.
func stopWhenOrphaned(dataDir string) {}
