//go:build !linux

package runtime

// denyProcEnvironReads is a no-op off Linux: the exposure it closes is
// /proc/<pid>/environ, which does not exist on macOS or Windows. Production
// runs Linux, so this only exists so the desktop build compiles and the call
// site stays unconditional.
func denyProcEnvironReads() error { return nil }
