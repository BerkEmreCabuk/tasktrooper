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
	WorkDir string
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
	// It is on the request rather than on the context because the context
	// overlay (application/toolchain) answers a different question: which
	// INSTALL on this machine a spawned process should find. This is what the
	// repository DECLARED, in the names a session's own version manager reads,
	// and only the executor that starts that session can apply it.
	//
	// Every name in it is one SessionEnvAllowed accepts and every value is in
	// the form the tool takes, so it is passed on UNCHANGED.
	//
	// Empty is a complete answer: the checkout declares nothing, and the session
	// runs on the machine's own defaults. It must never be filled in with a
	// "system" or "latest" default — none of those is something the repository
	// said.
	Env map[string]string
}
