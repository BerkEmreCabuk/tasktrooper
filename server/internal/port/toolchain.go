package port

import "context"

// ToolchainPin is one version declaration a checkout makes, with the file that
// made it.
type ToolchainPin struct {
	// Language is normalised and lowercase: go, node, python, ruby, java,
	// rust, flutter, dart — or whatever a `.tool-versions` line named,
	// unchanged.
	Language string
	// Version is what the file says, verbatim. A range stays a range.
	Version string
	// Exact is false for a constraint. It is the field that says whether
	// Version names one runtime or a set of them.
	Exact bool
	// Source is the file this came from, relative to the workspace.
	Source string
}

// Toolchain is what one checkout declares it needs.
type Toolchain struct {
	// Pins is every declaration found, in precedence order per language — a
	// version manager's own file first, then the language's dotfile, then a
	// manifest's constraint. Two files pinning one language BOTH appear: the
	// disagreement is reported rather than resolved silently, and the order
	// says which one to believe.
	Pins []ToolchainPin
	// Env is the exact pins, one per language, in the form the executor takes.
	// It is a projection of Pins rather than a second opinion about it.
	//
	// Empty is a complete answer — the checkout declares nothing — and must
	// not be turned into a default.
	Env map[string]string
}

// ToolchainDetector reads a checkout's own version declarations.
//
// It exists as a port because the answer depends on files this process may not
// be allowed to open: the implementation decides which directories are
// readable at all, and the application layer only ever names a workspace it
// already owns.
type ToolchainDetector interface {
	// Detect reads the pin files directly in workspace, which must be an
	// absolute path inside the workspace root.
	Detect(ctx context.Context, workspace string) (Toolchain, error)
	// Available reports whether detection can be asked for at all.
	Available() bool
}
