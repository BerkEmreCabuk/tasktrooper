package domain

// ChatExecution is one CHAT turn handed to an executor that is not the
// in-process agent loop. It is the conversational sibling of TaskExecution, and
// the differences between the two are the whole reason it is a separate type.
//
// A board task is a single shot: the runner assembles one history, the executor
// runs it to a closing message, and the run is over. A chat is a thread the user
// keeps typing into, which changes three things:
//
//   - Prompt exists. From the second turn on, the executor is resuming a live
//     CLI session that already holds the persona, the workspace and everything
//     said so far, so it must be sent only what is new. Re-flattening the whole
//     transcript on every turn would pay for the entire conversation again on
//     each message AND read to the model as a fresh instruction stacked on top
//     of its own memory.
//   - ResumeSessionID is the NORMAL case here, not the exception. On the board
//     it means "you were parked mid-work"; here it simply means "this is not the
//     first thing the user said".
//   - The answer is streamed. A board run's output is read once it is finished;
//     a chat's is watched as it appears, which is what port.ChatStream carries.
//
// History is still the full assembled context because the FIRST turn needs it,
// and because a resume that turns out to be impossible (the CLI forgot the
// session) has to be able to start over without another round trip to the
// caller.
type ChatExecution struct {
	// History is the turn's assembled context in the order the session service
	// built it: system blocks first (persona, KPI, memories, workspace), then
	// the conversation. Used when there is no session to resume — the first turn
	// of a chat, and the fallback when a resume is refused.
	History []Message
	// Prompt is the new user message on its own. Used when ResumeSessionID is
	// set; ignored otherwise, where the same text is already the tail of History.
	Prompt string
	// Model is the resolved model for this turn. Empty means "the executor's own
	// default", which for a subscription CLI is the right answer — see
	// ClaudeCodeModels, whose first option is exactly that.
	Model    string
	Provider LLMProviderType
	// Policy is the tool policy the loop would have enforced, already merged and
	// uplifted by the session service. It governs the TaskTrooper tools the
	// session is served over MCP; see the claudecode package on what it does not
	// govern.
	Policy ToolPolicy
	// WorkDir is the chat's workspace — the repository checkout for a
	// repository-scoped chat, the task's own branch checkout for a task-bound
	// one. Required, for the same reason TaskExecution.WorkDir is: a child
	// process has to be started somewhere, and the wrong somewhere is the shared
	// project root on whatever branch it happens to be on.
	WorkDir string
	// ResumeSessionID continues the CLI conversation this chat has been having.
	// Empty on the first turn.
	ResumeSessionID string
	// SessionID is the chat's own id, used to attribute the CLI session in logs
	// and at the MCP endpoint the way a board run is attributed by task key.
	SessionID string
}

// ChatResult is what a chat turn produced.
//
// It carries the CLI session id alongside the response because that id is the
// conversation's continuity and the caller is the only party that can persist
// it. An executor cannot: it has no store, and by design it holds nothing
// between calls.
type ChatResult struct {
	Response AgentResponse
	// CLISessionID is the session the NEXT turn must resume. It is set whenever
	// the CLI announced one — including on a turn that then failed, because a
	// session that exists is worth resuming even if this turn did not finish.
	CLISessionID string
	// Resumed reports whether this turn actually continued CLISessionID rather
	// than starting a new conversation. False after a resume was refused and the
	// turn fell back to a fresh session; the caller logs the difference, and a
	// test asserts on it.
	Resumed bool
}
