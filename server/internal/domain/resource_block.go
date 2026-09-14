package domain

// ResourceBlock is a tool saying "not now" about something the agent cannot
// influence: a shared piece of hardware someone else is holding.
//
// It is deliberately NOT a tool error. An error is a fact about the work — a
// bad selector, a crashed app — and the agent is expected to react to it. A
// held device is a fact about the queue: there is nothing to fix, nothing to
// retry, and a model that keeps trying only burns the run's budget waiting for
// a phone. So it travels the same path as a clarification request: the tool
// hands it back, the agent loop stops the turn, and the runner parks the task
// in the blocked column until the resource frees up.
//
// The difference from a clarification is who releases it. A question waits on a
// human answering in a chat; a resource waits on nobody, which is why it needs
// the periodic sweep (application/board/device_sweeper.go) rather than an
// inbound event.
type ResourceBlock struct {
	// Resource names the contended thing. It is the key the sweeper claims
	// parked tasks by, so it must match one of the Resource* constants.
	Resource string `json:"resource"`
	// Detail is the human-readable reason shown on the board card.
	Detail string `json:"detail,omitempty"`
}

// ResourceMobileDevice is the shared pool of physical Android phones behind the
// mobile_* tools.
//
// One resource for however many phones there are, deliberately. It names the
// QUEUE, not a device: a task parks here only when every phone is taken, and
// what wakes it is any one of them freeing up. Naming a device instead would
// make a task wait for the phone it happened to ask for while another sat idle,
// and would put a choice in the agent's hands that it has no way to make well.
const ResourceMobileDevice = "mobile_device"

// ResourceClaudeCodeQuota is the Claude subscription behind the local Claude
// Code CLI, parked on when that subscription's usage limit is reached.
//
// It is a resource in exactly the sense ResourceMobileDevice is — a shared
// thing the agent cannot influence, which frees up on its own — and it parks
// through the same board_tasks.blocked_resource column so the card reads the
// same way. What differs is who releases it: the device sweeper asks hardware
// whether it is free, while this one asks the clock, because the run that
// parked recorded the reset time on its own row (task_agent_runs.quota_resume_at,
// migration 101). See application/board/quota_sweeper.go.
const ResourceClaudeCodeQuota = "claude_code_quota"

// ResourceDeployWatch is the production deploy of a task's merge commit, parked
// on while that deploy is still running.
//
// It is a resource in the same sense the other two are — a thing the agent
// cannot influence, which resolves on its own — and it exists for one specific
// reason: a deploy takes minutes, and there is no acceptable way to spend them
// inside a tool call. A tool that slept would hold the run's LLM context, its
// concurrency slot and its MCP token open for the duration, and would still be
// wrong on the far end of the timeout. A tool that returned "pending" and
// nothing else would leave the agent to invent a retry loop, which is the same
// sleep paid for one LLM turn at a time.
//
// So the pending deploy parks exactly like a held phone: the tool returns a
// ResourceBlock, the loop stops the turn, the runner parks the card, and
// application/board/deploy_sweeper.go re-dispatches it once GitHub reports the
// deploy settled. The sweeper does the polling — one API call per parked task
// per pass, no agent run — so waiting costs nothing until there is something to
// say.
//
// Unlike the device it is per-TASK, not a queue: two tasks waiting on two
// deploys are waiting on two different things, and either may settle first. The
// sweeper therefore checks each parked task and claims only the ones whose
// deploy is done, instead of taking the oldest on a global probe.
const ResourceDeployWatch = "deploy_watch"

// ResourceWorkOrder is the completion of the tasks this one declares it must be
// worked after — its `blocks` relations, parked on while any of them is still
// open.
//
// It is the odd one out of the four in what it waits for: a phone, a quota and
// a deploy are all facts about the world outside the board, while this one waits
// on the board itself. It is parked the same way regardless, and for the same
// reason the others are — there is nothing for an agent to do about it, so
// dispatching a run to discover that would spend a model turn and a workspace to
// learn what the relation already said.
//
// What the park buys over simply refusing the dispatch is visibility and a way
// back. A refused dispatch leaves a card sitting in `todo` looking exactly like
// an unstarted one, with nothing to say why nobody picked it up; parking moves
// it to `blocked` with "waiting for T-12 (API migration) to finish" on the card
// and a system comment naming every blocker. The way back is
// application/board/work_order_sweeper.go, which asks the relation graph — not
// hardware, not a clock, not GitHub — whether the blockers have landed.
//
// Per-TASK, not a queue, so it releases like the deploy watch rather than like
// the device: two parked tasks are waiting for two different blockers, and the
// newer one's may well finish first.
const ResourceWorkOrder = "work_order"

// ResourceHumanDecision is the one park nothing automatic ever releases: the
// board has detected that it is repeating itself, and only a person can say
// what should happen next.
//
// It is the odd one out of the five. A phone frees up, a quota window rolls
// over, a deploy finishes, a blocker reaches done — each of those has a sweeper
// that asks the world whether the wait is over. This one has none ON PURPOSE.
// The two guards that park on it (application/board/pipeline_bounce_guard.go
// and review_loop_guard.go) fire exactly when the automatic answer has already
// been tried and produced the same result, so a sweeper that released the card
// would restart the loop it was parked to stop — the billing-blocked CI that
// bounced one task through eleven identical review cycles in forty minutes.
//
// The way out is a human: fixing what the loop was stuck on (paying the CI
// bill, answering the review) and dragging the card out of `blocked`, which is
// the same manual release every other park also accepts.
const ResourceHumanDecision = "human_decision"

// ResourceRunnerNotAttached is the assignee's Mac, parked on when the work
// needs it and it is not connected.
//
// It is the sixth, and it is a resource in exactly the sense the phone is: a
// physical thing outside the board that the agent cannot influence and that
// comes back on its own. What makes it worth its own name rather than a
// failure is that it is now the NORMAL state of half a working day. agent-server
// runs in the cloud and the code lives on a laptop; a task assigned to somebody
// whose lid is shut has nothing wrong with it, and failing its run would spend
// one of the task's three consecutive-failure lives on that, three times, and
// then stop the card for a human to look at a machine that was only asleep.
//
// Per-MEMBER, not per-task and not a queue: every card assigned to one person
// is waiting for exactly one laptop, and they all become runnable at the same
// instant. That is why the sweeper (application/board/runner_sweeper.go) probes
// once per distinct assignee rather than once per parked card, and releases
// every card belonging to a member whose Mac came back.
const ResourceRunnerNotAttached = "runner_not_attached"

// ValidResource guards what may be written to board_tasks.blocked_resource: an
// unknown value would park a task no sweeper ever looks for.
func ValidResource(name string) bool {
	return name == ResourceMobileDevice || name == ResourceClaudeCodeQuota ||
		name == ResourceDeployWatch || name == ResourceWorkOrder ||
		name == ResourceHumanDecision || name == ResourceRunnerNotAttached
}
