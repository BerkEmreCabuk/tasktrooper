package port

import "context"

// ToolchainPin is one version declaration a checkout makes, with the file that
// made it.
type ToolchainPin struct {
	// Normalised and lowercase.
	Language string
	// What the file says, verbatim; a range stays a range.
	Version string
	// False for a constraint — Version names a set of runtimes, not one.
	Exact bool
	// The file this came from, relative to the workspace.
	Source string
}

// Toolchain is what one checkout declares it needs.
type Toolchain struct {
	// In precedence order per language; two files pinning one language BOTH
	// appear — the disagreement is reported, and the order says which to
	// believe.
	Pins []ToolchainPin
	// The exact pins, one per language, in the form the executor takes. Empty
	// is a complete answer and must not be turned into a default.
	Env map[string]string
}

// ToolchainDetector reads a checkout's own version declarations. It exists as
// a port because the answer depends on files this process may not be allowed
// to open.
type ToolchainDetector interface {
	// Directly reads the pin files in workspace, which must be an absolute
	// path inside the workspace root.
	Detect(ctx context.Context, workspace string) (Toolchain, error)
	Available() bool
}
