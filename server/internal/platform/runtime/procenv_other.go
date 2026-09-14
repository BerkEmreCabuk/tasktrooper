//go:build !linux

package runtime

// denyProcEnvironReads is a no-op off Linux. The exposure it closes is
// /proc/<pid>/environ, which is a Linux procfs feature; macOS and Windows have
// no same-UID equivalent that hands a child the parent's environment block.
// Production runs Linux, so the real implementation is the one that matters —
// this exists so the desktop build compiles and the call site stays
// unconditional.
func denyProcEnvironReads() error { return nil }
