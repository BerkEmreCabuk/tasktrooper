package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type BoardConfigStore interface {
	GetSettings(ctx context.Context) (domain.BoardSettings, error)
	UpdateSettings(ctx context.Context, keyPrefix string) (domain.BoardSettings, error)

	ListColumns(ctx context.Context) ([]domain.BoardColumn, error)
	ReplaceColumns(ctx context.Context, columns []domain.BoardColumnInput) error

	ListMembers(ctx context.Context) ([]domain.BoardMember, error)
	SetMembers(ctx context.Context, agentIDs []uuid.UUID) error

	ListSubscriptions(ctx context.Context) ([]domain.BoardSubscription, error)
	SetSubscriptions(ctx context.Context, subs []domain.BoardSubscriptionInput) error

	ListAgentSubscriptions(ctx context.Context, agentID uuid.UUID) ([]string, error)
	SetAgentSubscriptions(ctx context.Context, agentID uuid.UUID, columnSlugs []string) error

	ListTransitions(ctx context.Context) ([]domain.BoardTransition, error)
	SetTransitions(ctx context.Context, transitions []domain.BoardTransition) error

	AgentsForColumn(ctx context.Context, columnSlug string, taskType string) ([]uuid.UUID, error)
	ValidateColumnSlug(ctx context.Context, slug string) (bool, error)
}

type TaskCommentStore interface {
	Create(ctx context.Context, comment domain.TaskComment) (domain.TaskComment, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskComment, error)
}

type BoardEventStore interface {
	Create(ctx context.Context, event domain.BoardEvent) (domain.BoardEvent, error)
	ListRecent(ctx context.Context, limit int) ([]domain.BoardEvent, error)
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.BoardEvent, error)
}

type TaskAgentRunStore interface {
	Create(ctx context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error)
	Update(ctx context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error)
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskAgentRun, error)
	ListRecent(ctx context.Context, limit int) ([]domain.TaskAgentRun, error)
	HasPendingForEvent(ctx context.Context, boardEventID, agentID uuid.UUID) (bool, error)
	// HasPendingForTask reports whether the agent already has a QUEUED (not yet
	// running) run for this task, regardless of which event created it. Every
	// board move mints a fresh event, so per-event dedupe alone lets each move
	// stack another pending run on the same task — this is the task-level cap.
	// Running runs are intentionally excluded: a run that finishes by pushing
	// the task back (verify gate → in_progress) must still be able to queue the
	// follow-up run while it is itself still marked running.
	HasPendingForTask(ctx context.Context, taskID, agentID uuid.UUID) (bool, error)
	// HasLiveForTask reports whether the agent has a run for this task that is
	// queued OR still executing. It is the cap for events that carry no new
	// state for the agent to act on — a comment, most of all. A comment left
	// while the agent is mid-run passed the pending-only check and queued a
	// second run on the same task, which then claimed it and dragged it back
	// out of code_review while the first run was still finishing.
	HasLiveForTask(ctx context.Context, taskID, agentID uuid.UUID) (bool, error)
	// ListStale returns runs still pending/running whose last update is older
	// than cutoff — dispatch is purely event-driven and the runner's job queue
	// is in-memory, so a run can get orphaned (process restart, dropped job)
	// with nothing else to ever revisit it.
	ListStale(ctx context.Context, cutoff time.Time) ([]domain.TaskAgentRun, error)
	// Touch bumps updated_at on a still-running run and reports the status it
	// found. Without the bump the row's timestamp freezes at the moment the run
	// started, so ListStale cannot tell "the pod died mid-run" from "the agent
	// is still working" — and every run longer than the stale window was failed
	// and re-dispatched while it was still making progress. Terminal runs are
	// left alone.
	//
	// It RETURNS the status because the heartbeat is also how a run learns it
	// has been stopped. The stop button writes 'cancelled' on this row from
	// whichever replica served the request; the replica actually executing the
	// run may be a different one, and its cancel func is in its own memory
	// where nothing else can reach it. Reading the status on the write that was
	// happening anyway turns "the row says stop" into a signal that crosses
	// processes, at no extra round trip.
	Touch(ctx context.Context, id uuid.UUID) (status string, err error)
	// ClaimRun is the atomic "may this process execute this run", and it is the
	// only way a run becomes 'running'. See RunClaim.
	ClaimRun(ctx context.Context, claim RunClaim) (RunClaimResult, error)
	// FailIfStale marks a run failed ONLY if its heartbeat is still older than
	// cutoff at the moment of the write. The reconciler reads a list and then
	// acts on it, and between those two steps the run's owner may have
	// heartbeated — on one replica that window was microseconds, across
	// replicas it is however long the sweep takes.
	FailIfStale(ctx context.Context, id uuid.UUID, cutoff time.Time, summary string) (bool, error)
	// HasLiveRunForTask reports whether any run of this task is executing with a
	// fresh heartbeat, on any replica. It replaces the in-process "is this task
	// active" map that anything wanting to disturb a task's workspace used to
	// ask — a map that answers only for the process holding it.
	HasLiveRunForTask(ctx context.Context, taskID uuid.UUID, liveWithin time.Duration) (bool, error)
	// GetByID returns a single run. Returns domain.ErrTaskAgentRunNotFound when
	// the row is gone.
	GetByID(ctx context.Context, id uuid.UUID) (domain.TaskAgentRun, error)
	// CancelIfLive marks a pending/running run cancelled and returns it. The bool
	// is false when the run had already stopped — the caller races the run
	// itself, and pressing stop twice must not undo a completed run. The status
	// write is the durable half of cancellation: the in-process context cancel is
	// best-effort, this row is what the UI and a restarted process both read.
	CancelIfLive(ctx context.Context, id uuid.UUID, reason string) (domain.TaskAgentRun, bool, error)
}

// RunClaim is the runner asking to execute one run.
//
// It exists because "one run per task" used to be a Go map
// (Runner.activeTasks), which is correct for one process and wrong as soon as
// two processes share a database.
//
// Answering it in one statement is not an optimisation. Asking "is the task
// free?" and then writing 'running' leaves a window two replicas can both pass
// through; the answer and the write have to be the same statement or they are
// not a claim.
//
// There is deliberately no concurrency budget: every run whose task is free
// starts immediately.
type RunClaim struct {
	RunID  uuid.UUID
	TaskID uuid.UUID
	// LiveWithin is how fresh a heartbeat has to be for a 'running' row to
	// count as holding its task. A run whose owner was SIGKILLed still has a
	// 'running' row until the reconciler gets to it, and treating that as live
	// would let one dead process hold its task hostage.
	LiveWithin time.Duration
}

// RunClaimResult says whether this process won, and if not, why. The reason is
// for the log and for nothing else: every refusal leaves the row 'pending',
// which the reconciler re-dispatches, so no caller has to act differently on one
// reason than another.
type RunClaimResult struct {
	Claimed bool
	// Reason is "" when Claimed. Otherwise one of: "not_pending" (somebody else
	// claimed it, or it was cancelled while it queued) or "task_busy".
	Reason string
}

// TaskColumnSpanStore is the ledger of "which agent held this task, in which
// column, for how long". It is the single source of truth for time-based KPIs
// and for deciding who a defect escape belongs to.
type TaskColumnSpanStore interface {
	// RecordMove closes the task's open span (if it is a different column) and
	// opens a new one. Moving to the column the task is already in is a no-op,
	// so a duplicated event cannot split one stay into two spans.
	RecordMove(ctx context.Context, repositoryID, taskID uuid.UUID, toColumn string, at time.Time) error
	// AttachAgent claims the task's open span for an agent. The first agent to
	// run in a span owns it; later calls are ignored.
	AttachAgent(ctx context.Context, taskID, agentID uuid.UUID) error
	// SetReviewVerdict records a reviewing agent's decision on the task's open
	// span in the given column.
	SetReviewVerdict(ctx context.Context, taskID uuid.UUID, column, verdict string) error
	// OpenSpan returns the task's current span. The bool is false when the task
	// has no open span.
	OpenSpan(ctx context.Context, taskID uuid.UUID) (domain.TaskColumnSpan, bool, error)
	// OwnersForTask maps each column the task passed through to the first agent
	// that worked it. Columns worked only by a human are absent.
	OwnersForTask(ctx context.Context, taskID uuid.UUID) (map[string]uuid.UUID, error)
	// HasVisited reports whether the task ever entered the given column.
	HasVisited(ctx context.Context, taskID uuid.UUID, column string) (bool, error)
	// LatestVerdicts maps every column the task has ever entered to the review
	// verdict recorded on its MOST RECENT visit there ("" when none was
	// recorded — verdicts only exist while require_human_review is on).
	//
	// It is HasVisited for the whole board in one round trip, plus the one
	// thing HasVisited cannot express: a task rejected at a gate and then
	// dragged forward without being re-reviewed has visited that column, but
	// did not pass it. Keyed by the latest visit on purpose — a rejection
	// followed by a fresh, unrejected visit is exactly the rework loop the
	// span ledger exists to allow.
	LatestVerdicts(ctx context.Context, taskID uuid.UUID) (map[string]string, error)
	// CleanTaskHours returns, for each task the agent worked in the given
	// columns that completed cleanly inside the window, the total hours it held
	// that task. One element per task: repeat visits are summed, and the caller
	// takes the median.
	CleanTaskHours(ctx context.Context, agentID uuid.UUID, columns []string, from, to time.Time) ([]float64, error)
}
