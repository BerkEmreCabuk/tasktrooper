package board

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// maxReviewLoopEntries is how many times a task may land in need_revision with
// no human having touched it in between before the board stops dispatching the
// developer and asks for a person instead.
//
// THREE, and the number is the smallest one that cannot fire on healthy work:
//
//   - once is the ordinary case. A reviewer found something, the developer
//     fixes it, the task moves on. Capping at one would break the feature.
//   - twice happens and is fine. The fix was incomplete, or the reviewer's
//     second pass found something the first did not — that is review working.
//   - three times with NOBODY having intervened is the signature of a machine
//     talking to itself. By then the same two agents have exchanged the same
//     task three times over, and every real instance of it seen so far (the
//     billing-blocked CI loop, a QA verdict the developer's run cannot act on)
//     produced identical rounds, not converging ones.
//
// "Since the last human event" is what makes the threshold safe to set this
// low: any human touch at all — a comment, a drag, an answered clarification —
// resets it. A person actively working a hard task can send it back as many
// times as they like; only the unattended board is capped.
const maxReviewLoopEntries = 3

// reviewLoopHistoryDepth is how much of a task's board history one check reads.
// 500 is the postgres store's own ceiling for ListByTask, so asking for more
// would silently get this anyway.
const reviewLoopHistoryDepth = 500

// TaskEventHistory reads one task's board events. port.BoardEventStore,
// narrowed to the single call the guard makes.
type TaskEventHistory interface {
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.BoardEvent, error)
}

// ReviewLoopGuard is the general brake on review ping-pong.
//
// PipelineBounceGuard closes one specific loop — the same commit failing CI
// forever — by recognising its cause. This one closes the SHAPE, whatever the
// cause: a task that keeps arriving in need_revision without a human ever
// weighing in is not being reviewed, it is being circulated. The production
// case that motivated it had two variants running side by side (a red pipeline
// bouncing the card, and QA sending it back to a developer run that found
// nothing to change and handed it straight on again), and only one of them has
// a pipeline anywhere in it.
//
// So the counted signal is deliberately dumb and cause-free: entries into
// need_revision, since the last board event any human produced. It cannot know
// why the task keeps coming back, and it does not need to — three unattended
// rounds is already the evidence that the board's own participants cannot
// finish it.
//
// It runs in Dispatcher.Dispatch, after the event row is written, for the same
// reason every other gate there does: board history must record what happened
// whatever the gate decides.
type ReviewLoopGuard struct {
	events   TaskEventHistory
	parker   ResourceParker
	comments TaskCommenter
	parks    *ParkJournal
}

func NewReviewLoopGuard(events TaskEventHistory, parker ResourceParker) *ReviewLoopGuard {
	return &ReviewLoopGuard{events: events, parker: parker}
}

// SetCommenter attaches the store used to explain the park on the card. Nil-safe:
// without it the park still happens and still shows in the blocked badge.
func (g *ReviewLoopGuard) SetCommenter(c TaskCommenter) {
	if g != nil {
		g.comments = c
	}
}

// SetParkJournal attaches the writer that records the park as a board move.
// Nil-safe: without it the card still reaches `blocked`, the timeline just does
// not show how it got there and the column span stays open on need_revision.
func (g *ReviewLoopGuard) SetParkJournal(j *ParkJournal) {
	if g != nil {
		g.parks = j
	}
}

// Hold reports whether this dispatch must be dropped because the task is going
// round in circles.
//
// currentEventID is the event Dispatch just wrote for this very move. It is
// passed so the guard can prove its history window actually reaches the
// present: ListByTask is oldest-first with a limit, so on a task with more
// events than the depth the window is the WRONG end of history, and counting
// old rounds there could park a task that is fine today. Not finding the
// current event is exactly that condition, and it fails open.
//
// uuid.Nil counts as not finding it. A caller with no event id cannot prove the
// window reaches the present either, and treating "no proof" as "proof" is the
// same bug with an extra step — it would let a caller that never wrote an event
// park a task on the strength of a window that might be years old.
func (g *ReviewLoopGuard) Hold(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, currentEventID uuid.UUID) bool {
	if g == nil || g.events == nil {
		return false
	}
	history, err := g.events.ListByTask(ctx, task.ID, reviewLoopHistoryDepth)
	if err != nil {
		// Fail open, unlike the work-order gate above. That one fails closed
		// because starting work on an unknown order is unrecoverable; this one
		// only ever SUPPRESSES a dispatch, so an unreadable history must not be
		// allowed to stall a task that is progressing normally.
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("review loop guard: reading board history failed, dispatching normally")
		return false
	}
	if currentEventID == uuid.Nil || !containsEvent(history, currentEventID) {
		log.Warn().Str("task_id", task.ID.String()).
			Msg("review loop guard: board history window does not reach this move, dispatching normally")
		return false
	}
	entries := reviewLoopEntries(history)
	if entries < maxReviewLoopEntries {
		return false
	}

	log.Warn().
		Str("task_id", task.ID.String()).
		Int("need_revision_entries", entries).
		Msg("review loop guard: task sent back repeatedly with no human input, parking it")

	// Park FIRST, then explain it — and only if the park took.
	//
	// The commenter is repository.Service, whose AddComment emits task.commented
	// straight back into Dispatch, which resolves the column's agents for it and
	// enqueues a run. On a card still sitting in need_revision that comment
	// starts the very developer run this cap just refused to start. Once the
	// card is `blocked`, isDispatchSuspendedTask drops that dispatch and the
	// comment is inert.
	//
	// The park is also what keeps the comment to one, which an `entries ==
	// maxReviewLoopEntries` equality only appeared to do: with the park failing,
	// every later sweep found the same count and commented again. A held task
	// that is not parked is not held at all — nothing stops the next event
	// dispatching it — so a failed park gives up the hold and lets the ordinary
	// dispatch through rather than dropping runs silently forever.
	if !g.park(ctx, repositoryID, task, entries) {
		return false
	}
	g.comment(ctx, repositoryID, task, entries)
	return true
}

// reviewLoopEntries counts how many times the task entered need_revision since
// the last board event a human produced.
//
// Walked newest-first, stopping at the first human event: that event is the
// reset, and everything before it belongs to a round a person has already seen
// and responded to. The history arrives oldest-first (the store's ORDER BY), so
// the walk runs backwards over it.
func reviewLoopEntries(history []domain.BoardEvent) int {
	n := 0
	for i := len(history) - 1; i >= 0; i-- {
		event := history[i]
		payload := decodeEventPayload(event.Payload)
		if eventIsHuman(event, payload) {
			return n
		}
		if event.EventType != domain.BoardEventTaskMoved {
			continue
		}
		if col, ok := movedIntoColumn(payload); ok && col == string(domain.TaskColumnNeedRevision) {
			n++
		}
	}
	return n
}

// eventIsHuman reports whether a person caused this event.
//
// Two independent signals, either of which is enough. The payload actor is
// what repository.Service stamps on a move it was told a human made; the
// actor_user_id column is the signed-in uid Dispatch resolves from the request
// context. They come from different layers and neither is present on every
// human event — a comment written through the API carries the uid but no
// actor key, a tool-driven move carries the key but no uid — so reading only
// one of them would miss half of all human involvement and cap tasks people
// are actively working on.
func eventIsHuman(event domain.BoardEvent, payload map[string]interface{}) bool {
	if event.ActorUserID != nil && *event.ActorUserID != "" {
		return true
	}
	actor, _ := payload[domain.EventPayloadActor].(string)
	if actor == domain.EventActorHuman {
		return true
	}
	// A human comment arrives as task.commented with the author fields set;
	// actorAgentIDFromPayload reads the same pair to decide the opposite
	// question (was this an AGENT's own comment).
	if event.EventType == domain.BoardEventTaskCommented {
		if authorType, _ := payload["author_type"].(string); authorType == "user" || authorType == "human" {
			return true
		}
	}
	return false
}

// movedIntoColumn names the column a move event landed the task in — and only
// when the event really was a column CHANGE.
//
// `to_column` is what repository.Service writes when it moves a card, and it is
// the only key trusted here. Dispatch's own `column` stamp looks like the same
// thing and is not: it goes on EVERY task.moved event Dispatch writes, including
// the synthetic ones nobody moved anything for —
//
//	the reconciler re-dispatching an idle assigned task (reconciler.go, payload
//	`reconciled`), and the quota / work-order / deploy sweepers waking a parked
//	one (payload `resumed`, and `resumed_resource` for the two that carry it).
//
// Each of those is Dispatch stamping the column the task is ALREADY in. Reading
// them as arrivals meant a task sitting in need_revision with a failing dev run
// would be parked after three reconciler sweeps without ever having looped —
// the guard firing on its own board's housekeeping.
//
// The resume/reconcile keys are checked as well as the to_column requirement,
// belt and braces: those payloads carry no to_column today, and the day one of
// them grows a "moved it back where it was" field is not the day this cap should
// start counting.
//
// A from/to pair that is identical is a hand-off rather than a move (the
// pipeline handing a card to its reviewer emits exactly that), and counting it
// would inflate the streak with events that changed nothing.
func movedIntoColumn(payload map[string]interface{}) (string, bool) {
	if payload == nil {
		return "", false
	}
	for _, synthetic := range []string{"reconciled", "resumed", domain.EventPayloadResumedResource} {
		if _, ok := payload[synthetic]; ok {
			return "", false
		}
	}
	to, ok := payload["to_column"].(string)
	if !ok || to == "" {
		return "", false
	}
	if from, _ := payload["from_column"].(string); from == to {
		return "", false
	}
	return to, true
}

// lastHumanEventAt is when a person last touched this task, read off an
// oldest-first board history.
//
// Shared with PipelineBounceGuard, which needs the same reset for the same
// reason: both guards suppress something a human would otherwise be told, and
// both must stop suppressing it the moment there is a human in the loop. Same
// walk as reviewLoopEntries — backwards, stopping at the first human event —
// so the two guards can never disagree about where the reset is.
func lastHumanEventAt(history []domain.BoardEvent) (time.Time, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		event := history[i]
		if eventIsHuman(event, decodeEventPayload(event.Payload)) {
			return event.CreatedAt, true
		}
	}
	return time.Time{}, false
}

func decodeEventPayload(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}

func containsEvent(history []domain.BoardEvent, id uuid.UUID) bool {
	for _, event := range history {
		if event.ID == id {
			return true
		}
	}
	return false
}

// reviewLoopGateApplies reports whether this dispatch is one the cap has
// anything to say about: a task ARRIVING in need_revision.
//
// Only that column, and only a move INTO it, read off the same payload pair the
// counter itself trusts. "task.moved plus the task is in need_revision" is not
// that: Dispatch stamps task.moved on its own housekeeping too, so a reconciler
// revival or a quota resume of a card ALREADY sitting in need_revision passed
// the gate and got counted against a streak it was no part of. The counter
// (movedIntoColumn) has always excluded those events; the gate letting them in
// meant the cap could fire on a dispatch that was not a lap at all.
//
// A comment on a task already in need_revision, an assignment, a hand-off whose
// from and to are the same column: none of them is another lap, and suppressing
// them would strand a task the guard has not even decided about yet.
func reviewLoopGateApplies(input DispatchInput) bool {
	if input.EventType != domain.BoardEventTaskMoved {
		return false
	}
	if input.Task.Column != domain.TaskColumnNeedRevision {
		return false
	}
	to, ok := movedIntoColumn(input.Payload)
	return ok && to == string(domain.TaskColumnNeedRevision)
}

// comment says on the card what the board stopped doing and why, in the same
// place every other system explanation goes.
func (g *ReviewLoopGuard) comment(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, entries int) {
	if g.comments == nil {
		return
	}
	if _, err := g.comments.AddComment(ctx, repositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content: fmt.Sprintf("review loop: sent back %d times without human input; parking for a human decision. "+
			"Aradaki turlarda hiçbir insan bu karta dokunmadı ve sonuç değişmedi, bu yüzden geliştirici tekrar "+
			"başlatılmadı. Kart `blocked` kolonunda bekliyor: ne yapılması gerektiğine karar verip elle ilerletin.", entries),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review loop guard: park comment failed")
	}
}

// park moves the card into `blocked` on the human-decision resource, records
// that move in board history, and reports whether the card actually got there.
//
// The journalled move is need_revision → blocked, which is a SECOND, distinct
// move from the one Dispatch already wrote to get the card here — that one
// landed it in need_revision, this one takes it out again. Without the record
// the card jumps to `blocked` with nothing saying when or why, and, worse, the
// column span opened for need_revision never closes: a task parked for two days
// reads as two days of active revision in every time-in-column KPI. That open
// span is why MoveReasonReviewLoopParked exists, and journalling here is what
// makes the constant mean anything.
func (g *ReviewLoopGuard) park(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, entries int) bool {
	if g.parker == nil || task.Column == domain.TaskColumnBlocked {
		return false
	}
	detail := fmt.Sprintf("review loop: sent back %d times with no human input — waiting for a human decision", entries)
	previous, err := g.parker.BlockOnResource(ctx, repositoryID, task.ID, domain.ResourceHumanDecision, detail)
	if err != nil {
		// Logged rather than returned — the move that reached here did not fail
		// and must not look failed to its caller — but reported, because a card
		// that did not reach `blocked` is still dispatchable and suppressing its
		// dispatch would only make it go quiet.
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("review loop guard: parking the task failed; dispatching normally instead")
		return false
	}
	g.parks.Record(ctx, repositoryID, task, previous,
		domain.ResourceHumanDecision, domain.MoveReasonReviewLoopParked)
	return true
}
