package port

import (
	"context"
	"encoding/json"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RunnerCall is one RPC to a member's Mac.
//
// The wire lives in adapter/runner; this is the shape the application and the
// executors are allowed to know about, so nothing above the adapter layer has
// to import an HTTP client to ask a laptop to do something.
type RunnerCall struct {
	// Method is one of the runner's four: claude.run, workspace.prepare,
	// embeddings.create, preflight.report. The list is NOT duplicated as
	// constants here — the Mac refuses an unknown method with
	// `unsupported_method`, and a second copy of the list on this side would be
	// a second thing to keep in step with a program in another repository.
	Method string
	// Params is marshalled straight into the request line.
	Params any
	// ParamsOwnID says this method's own grammar uses `id` for something else,
	// so the transport must not splice its call id over it.
	//
	// It exists for exactly one collision and it is not hypothetical:
	// `mobile.boot` and `mobile.shutdown` take `{kind, id}` where `id` is the
	// DEVICE — a simctl UDID or an AVD name — and the transport's own `id` is
	// what a cancel names. Spliced, the Mac would decode our call id as the
	// device to boot and refuse a UUID that matches no simulator.
	//
	// The cost is that such a call cannot be cancelled BY ID, and that is the
	// correct trade rather than a regression: POST /cancel names a claude.run
	// and only a claude.run, and every other method is cancelled by the caller
	// closing the connection — which for a boot stops the wait, though not the
	// device that has already come up.
	ParamsOwnID bool
	// OnOutput receives each streamed `output` event as it arrives. nil
	// discards them, which is what a short call wants; `claude.run` is the
	// reason this field exists at all.
	OnOutput func(stream, data string)
}

// RunnerTransport carries a call to the Mac of one member of the tenant on
// ctx. The tenant is NOT a parameter for the same reason it is not one on any
// store method: it is a property of the request being served, and a caller
// that had to pass it could pass the wrong one.
//
// A call to a member with no Mac attached comes back as *domain.RunnerBlock,
// which the board parks on rather than fails — see domain/runner_block.go.
type RunnerTransport interface {
	Do(ctx context.Context, memberUID string, call RunnerCall) (json.RawMessage, error)
	// Configured reports whether there is a control plane to reach a Mac
	// through at all. False on self-hosted and desktop builds, where the CLI
	// runs in this same process and there is nothing to forward to.
	Configured() bool
}

// PreparedWorkspace is where a repository landed on somebody's Mac.
type PreparedWorkspace struct {
	// Path is absolute ON THE MAC. It is worth recording (task_agent_runs
	// .workspace_path, so a person can find the checkout) and worth nothing
	// else: no code in this process may open it.
	Path string
	// Rel is the same place relative to that Mac's workspace root, and is the
	// only form `claude.run` accepts. Everything downstream passes this.
	Rel string
	// Branch is what is actually checked out, which is not always what was
	// asked for: a task branch that does not exist on origin yet cannot be
	// checked out, and the session cuts it itself.
	Branch string
	// Head is the commit the checkout is on.
	Head string
	// Cloned is true when this call created the checkout rather than refreshing
	// one that was already there.
	Cloned bool
}

// WorkspacePreparer puts a repository on the machine that is going to edit it.
//
// It replaces the clone-into-DATA_DIR that ran here when agent-server WAS the
// machine with the code. In the cloud that directory is wrong by construction:
// nothing in this process can usefully hold a working copy, because the
// `claude` session that edits it runs on a laptop.
type WorkspacePreparer interface {
	// Prepare clones or refreshes repoURL into dir (relative, one or more safe
	// path segments) on memberUID's Mac and checks out branch. An empty branch
	// means the remote's default.
	Prepare(ctx context.Context, memberUID, repoURL, dir, branch string) (PreparedWorkspace, error)
	// Available reports whether workspaces can be prepared remotely at all.
	// False keeps the local git path exactly as it was.
	Available() bool
}

// ToolchainDetector asks the machine that HOLDS a checkout what that checkout
// declares it needs.
//
// It exists because the question cannot be answered anywhere else. The
// application/toolchain parsers resolve against a directory, and on a remote
// run that directory is a path relative to somebody's Mac — resolved in this
// process it names nothing, so a repository pinning its own Node or Python was
// honoured locally and silently ignored remotely.
//
// The returned map is passed to the executor UNCHANGED. Every name in it is
// already one the runner's own allowlist accepts and every value is already in
// the form the tool takes, so a mapping table on this side would be a second
// source of truth that can only drift.
type ToolchainDetector interface {
	// Detect returns the environment for a session in workspace on memberUID's
	// Mac. An empty map is a complete answer — the checkout declares nothing —
	// and must not be turned into a default.
	Detect(ctx context.Context, memberUID, workspace string) (map[string]string, error)
	// Available reports whether detection can be asked for at all. False leaves
	// the local resolver exactly as it was.
	Available() bool
}

// EmbeddingHostProbe answers "can the acting member's Mac produce an embedding
// right now, and if not, which of the two fixable things is wrong".
//
// It is a port rather than a direct dependency for the same reason
// WorkspacePreparer above is: the application layer decides WHETHER the
// question is worth asking (only when this tenant's embeddings resolve to a
// Mac at all), and the transport that asks it exists only in a cloud build.
// Available() false is a complete, correct answer meaning "there is no laptop
// in this deployment's embedding path", not a failure.
type EmbeddingHostProbe interface {
	// Probe reads the member on ctx. A returned error means the question could
	// not be ASKED — the caller reports domain.EmbeddingHostUnknown, never
	// "broken", because it has learned nothing about the Mac.
	Probe(ctx context.Context) (domain.EmbeddingHostStatus, error)
	Available() bool
}
