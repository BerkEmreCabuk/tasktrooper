package domain

// ChatExecution is one CHAT turn handed to an executor that is not the
// in-process agent loop — the conversational sibling of TaskExecution. A board
// task is a single shot; a chat is a thread the user keeps typing into, which
// changes three things: Prompt exists (from the second turn the executor is
// resuming a live CLI session that already holds everything said so far, so it
// must be sent only what is new); ResumeSessionID is the NORMAL case, not the
// exception; and the answer is streamed rather than read once finished
// (port.ChatStream). History is still the full context because the FIRST turn
// needs it, and a resume that turns out impossible has to be able to start over
// without another round trip.
type ChatExecution struct {
	// History is the turn's assembled context in the order the session service
	// built it: system blocks first (persona, KPI, memories, workspace), then
	// the conversation. Used when there is no session to resume.
	History []Message
	// Prompt is the new user message on its own. Used when ResumeSessionID is
	// set; ignored otherwise, where the same text is already the tail of
	// History.
	Prompt string
	// Model is the resolved model for this turn. Empty means "the executor's
	// own default", which for a subscription CLI is the right answer.
	Model    string
	Provider LLMProviderType
	// Policy is the tool policy the loop would have enforced, already merged
	// and uplifted by the session service; it governs the TaskTrooper tools the
	// session is served over MCP (see the claudecode package for what it does
	// not govern).
	Policy ToolPolicy
	// WorkDir is the chat's workspace — the repository checkout for a
	// repository-scoped chat, the task's own branch checkout for a task-bound
	// one. Required for the same reason TaskExecution.WorkDir is: a child
	// process has to be started somewhere, and the wrong somewhere is the shared
	// project root on whatever branch it happens to be on.
	WorkDir string
	// ResumeSessionID continues the CLI conversation this chat has been having;
	// empty on the first turn.
	ResumeSessionID string
	// SessionID is the chat's own id, used to attribute the CLI session in logs
	// and at the MCP endpoint the way a board run is attributed by task key.
	SessionID string
}

// ChatResult is what a chat turn produced. It carries the CLI session id
// alongside the response because that id is the conversation's continuity and
// the caller is the only party that can persist it — an executor has no store
// and by design holds nothing between calls.
type ChatResult struct {
	Response AgentResponse
	// CLISessionID is the session the NEXT turn must resume, set whenever the
	// CLI announced one — including on a turn that then failed, because a
	// session that exists is worth resuming even if this turn did not finish.
	CLISessionID string
	// Resumed reports whether this turn actually continued CLISessionID rather
	// than starting a new conversation; false after a resume was refused and
	// the turn fell back to a fresh session.
	Resumed bool
}
