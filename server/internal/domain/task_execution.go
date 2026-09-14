package domain

// TaskExecution is one board task handed to an executor that is NOT the
// in-process agent loop.
//
// It carries the same four things the loop is called with (history, model,
// provider, policy) plus the two an out-of-process executor cannot derive for
// itself:
//
//   - WorkDir, because the loop's tools read the workspace off the run context
//     while a child process has to be started IN it. This is the task workspace
//     the runner already cloned and checked the tt-<key> branch out in, never
//     the shared project root.
//   - ResumeSessionID, because a run that was parked mid-work (see QuotaBlock)
//     resumes the executor's own session rather than starting the task over.
//
// It is a domain value object rather than a struct in the board package so the
// adapter implementing port.TaskExecutor does not have to import the
// application layer to be called.
type TaskExecution struct {
	// History is the run's assembled context in the order the runner built it:
	// system blocks first (persona, workspace, project, memories), the trigger
	// as the single user message, then this task's own evidence. An executor
	// that cannot send a message list flattens it — see the claudecode adapter,
	// which folds every system block into one --append-system-prompt and sends
	// the user content as the prompt.
	History []Message
	// Model is what the run should use, already resolved through any
	// task_type_models override. Empty means "the executor's own default",
	// which for a subscription CLI is the right answer.
	Model    string
	Provider LLMProviderType
	// MaxTurns caps the executor session, already resolved from the agent
	// record. 0 means "the executor's own default", the same convention Model
	// uses two fields up.
	MaxTurns int
	// Effort is the CLI effort level (low, medium, high, xhigh, max), resolved
	// from the agent record. Empty means the executor's default. An executor
	// with no such notion ignores it, as it ignores a Model it cannot switch.
	Effort string
	// Policy is the tool policy the loop would have enforced. An executor that
	// cannot enforce it must say so rather than pretend — see the claudecode
	// adapter's note on the MCP tool surface.
	Policy ToolPolicy
	// WorkDir is the task workspace. Required: an executor must never fall back
	// to the shared project root, where it would work on another task's branch.
	//
	// It is an absolute path for an executor that runs the process here, and
	// the path RELATIVE to that Mac's workspace root for one that runs it on a
	// laptop — the only form the runner accepts, and the only form that means
	// anything on a machine whose home directory this process has never seen.
	// Whoever prepared the workspace decides which, because only they know
	// where it landed.
	WorkDir string
	// MemberUID is WHOSE Mac this run belongs on: board_tasks.assignee_user_id
	// (migration 115), the Firebase uid of the person the card is assigned to.
	//
	// Empty on every executor that runs in this process — self-hosted, the
	// desktop bundle, the router's scratch-directory runs — and required by the
	// one that does not. It is on the request rather than on the context
	// because it is a property of the TASK, not of the caller: a run is
	// dispatched by a sweeper or a webhook as often as by the assignee, and
	// taking it from the acting identity would send Ayşe's task to whoever
	// happened to trigger it.
	MemberUID string
	// SkillsOnDisk says the caller wrote this agent's skills into WorkDir as
	// files the CLI discovers by itself, so the session must not ALSO be offered
	// the tool that fetches a skill body over MCP — two mechanisms for one job,
	// and the model would pay a round trip to re-read what it can already see.
	//
	// It travels with the request rather than being assumed by the executor
	// because the executor cannot see it: only the caller that materialised the
	// workspace knows it happened. The board runner sets it from the same
	// condition it sets prompt.SkillsOnDisk from; a caller that materialises
	// nothing — the agent router's scratch-directory CLI runs — leaves it false
	// and keeps the tool. Setting it without materialising takes the run's only
	// access to its skills away, silently.
	SkillsOnDisk bool
	// ResumeSessionID continues a parked executor session. Empty starts fresh.
	ResumeSessionID string
	// TaskKey is the human-readable key (tt-123 style) used in logs and in the
	// short continue prompt a resumed session is given.
	TaskKey string
	// TaskTitle is the card's title, used for the same two purposes as TaskKey.
	TaskTitle string
	// Env is extra environment for the session, resolved by whoever can see the
	// checkout.
	//
	// It is on the request rather than on the context because the two paths
	// resolve it in two different places and neither may reach into the other's.
	// An executor that runs the process HERE reads the overlay off the context
	// (application/toolchain resolved it against a directory on this
	// filesystem); an executor that runs it on a laptop is handed this, which
	// the runner's own toolchain.detect produced by reading the pin files where
	// they actually are. A cloud process cannot do the first — the path names
	// nothing here — and a laptop's answer has no business overriding a local
	// run's own resolution.
	//
	// Every name in it is one the far side's allowlist accepts and every value
	// is in the form the tool takes, so it is passed on UNCHANGED. A mapping
	// table on this side would be a second source of truth that can only drift
	// from the parser that produced the value.
	//
	// Empty is a complete answer: the checkout declares nothing, and the session
	// runs on the machine's own defaults. It must never be filled in with a
	// "system" or "latest" default — none of those is something the repository
	// said.
	Env map[string]string
}
