package board

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// PipelineBounceGuard stops the board bouncing one task off the SAME failed
// commit forever.
//
// The loop it exists for, observed on task B-4 over forty minutes: the org's
// GitHub Actions were billing-blocked, so every check on the PR failed
// instantly ("job was not started because recent account payments have
// failed") with no commit ever changing. The board did exactly what it is
// built to do with a red build — PipelineRunner.reportPipelineFailure sent the
// card back — the reviewer re-approved it, QA re-approved it, entering
// code_review triggered a FRESH pipeline for the identical head SHA, that one
// failed identically, and the card came back again. Eleven cycles, ~40 agent
// runs, all on the user's paid quota, all of them re-litigating a commit
// nobody had touched.
//
// Every individual step in that cycle is correct in isolation. What is missing
// is the observation that ties them together: the second failure carried no
// information the first one did not. So this guard asks one question before a
// bounce — has this task ALREADY been sent back for a failed pipeline on this
// exact commit, with no human weighing in since? — and when the answer is yes
// it refuses to move the card at all, says so once, and parks it for a human.
//
// The state it reads is the pipeline history itself (task_pipelines rows carry
// head_sha since the gate resolver needed it), not a counter in memory. That
// matters twice over: the loop spans process restarts and webhook deliveries
// that land on whichever pod answers, and an in-memory brake would forget the
// streak on every deploy — which is the same as not having one.
type PipelineBounceGuard struct {
	pipelines PipelineHistoryReader
	tasks     TaskCommenter
	parker    ResourceParker
	parks     *ParkJournal
	// events supplies the human-touch reset. Optional: without it the streak
	// is counted over the task's whole pipeline history, which is what this
	// guard did before the reset existed.
	events TaskEventHistory
	// reader re-reads the task at park time. The task on the pipeline job is a
	// snapshot from when the pipeline was QUEUED, and a pipeline settling half
	// an hour later must not act on it — see parkTarget.
	reader TaskRunbookReader
}

// PipelineHistoryReader lists a task's pipeline rows. port.TaskPipelineStore,
// narrowed to the one read the guard makes.
type PipelineHistoryReader interface {
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskPipeline, error)
}

// NewPipelineBounceGuard wires the guard. The pipeline history reader is what
// it reasons from; the parker is what it acts with, and without one there is no
// hold at all — a suppressed bounce with no park leaves a dispatchable card that
// says nothing and stops nothing, so the guard steps aside instead.
func NewPipelineBounceGuard(pipelines PipelineHistoryReader, tasks TaskCommenter, parker ResourceParker) *PipelineBounceGuard {
	return &PipelineBounceGuard{pipelines: pipelines, tasks: tasks, parker: parker}
}

// SetParkJournal attaches the writer that makes the park visible in board
// history. Nil-safe, exactly as it is on Runner: without it the card still
// moves to blocked, the timeline just does not show the move.
func (g *PipelineBounceGuard) SetParkJournal(j *ParkJournal) {
	if g != nil {
		g.parks = j
	}
}

// SetEventHistory attaches the board-event reader used to find the last human
// touch. Nil-safe: without it the streak never resets on human involvement,
// which is the behaviour the guard shipped with.
func (g *PipelineBounceGuard) SetEventHistory(e TaskEventHistory) {
	if g != nil {
		g.events = e
	}
}

// SetTaskReader attaches the re-read used before parking. Nil-safe: without it
// the guard acts on the snapshot the pipeline was queued with, which is what it
// did before — and which is wrong for any pipeline that took long enough for a
// human, a sweeper or another guard to have moved the card meanwhile.
func (g *PipelineBounceGuard) SetTaskReader(r TaskRunbookReader) {
	if g != nil {
		g.reader = r
	}
}

// bounceAction is what the guard is allowed to DO to the board once it has
// decided this failure is a repeat. Deciding it and acting on it are separate
// questions: the first is about the pipeline history, the second about what the
// card looks like right now.
type bounceAction int

const (
	// bounceFailOpen: the guard could not establish the present state of the
	// task, so it does nothing at all and the ordinary bounce goes ahead.
	bounceFailOpen bounceAction = iota
	// bouncePark: park the card, then explain the park on it.
	bouncePark
	// bounceExplain: somebody else's park already holds the card. Say why the
	// board stopped, but leave their resource alone.
	bounceExplain
	// bounceSilent: hold, and touch nothing.
	bounceSilent
)

// Hold reports whether this failed pipeline must NOT bounce the task.
//
// true means the caller does nothing further: no comment about the failure, no
// move, no re-dispatch. The guard has already said whatever needed saying and
// has parked the card if it could.
//
// It fails OPEN on every uncertainty — no guard wired, no head SHA recorded, an
// unreadable pipeline history, a board history whose window cannot be shown to
// reach the present, a task it could not re-read, a park that did not take —
// because the thing it suppresses (telling a developer their build is red) is
// the correct default. A guard that fired on a bad read would silently swallow
// real failures, which is a far worse bug than the loop it prevents. Holding
// anyway on those reads was worse still: the card went quiet with nothing on it
// and nothing holding it, which is one more lap plus a lost failure report.
func (g *PipelineBounceGuard) Hold(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, pipeline domain.TaskPipeline) bool {
	if g == nil || g.pipelines == nil {
		return false
	}
	headSHA := strings.TrimSpace(pipeline.HeadSHA)
	if headSHA == "" {
		// Nothing to compare against: this pipeline cannot say which commit it
		// was about, so it cannot be shown to be a repeat of another one.
		return false
	}
	history, err := g.pipelines.ListByTask(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("pipeline bounce guard: reading the task's pipeline history failed, letting the bounce through")
		return false
	}
	since, ok := g.humanTouchWindow(ctx, task.ID, pipeline.CreatedAt)
	if !ok {
		return false
	}
	prior := priorSameCommitFailures(history, pipeline, headSHA, since)
	if prior == 0 {
		// The first red build on a commit is news. It goes back to the
		// developer exactly as it always did.
		return false
	}

	log.Warn().
		Str("task_id", task.ID.String()).
		Str("pipeline_id", pipeline.ID.String()).
		Str("head_sha", headSHA).
		Int("prior_failures", prior).
		Msg("pipeline bounce guard: same commit failed again, refusing to cycle the task")

	// What may be DONE to the board is decided on the current task, not on the
	// snapshot this pipeline was queued with.
	target, action := g.parkTarget(ctx, repositoryID, task)
	switch action {
	case bounceFailOpen:
		return false
	case bouncePark:
		// Park FIRST, and comment only if the park took.
		//
		// The commenter is repository.Service: AddComment emits task.commented,
		// Dispatch resolves the column's agents for it and enqueues a run. On a
		// card still sitting in code_review that comment starts the very agent
		// run this guard exists to prevent — the guard funding its own loop.
		// Once the card is in `blocked`, isDispatchSuspendedTask drops that
		// dispatch and the comment is inert. A park that failed leaves the card
		// dispatchable, so there is nothing safe to say on it.
		if !g.park(ctx, repositoryID, target, headSHA) {
			return false
		}
		g.comment(ctx, repositoryID, target, pipeline, headSHA)
	case bounceExplain:
		g.comment(ctx, repositoryID, target, pipeline, headSHA)
	}
	return true
}

// humanTouchWindow is when a person last did anything to this task, and whether
// that answer can be trusted at all.
//
// It is the same reset the review-loop cap uses, and for the same reason: a
// human who has looked at the card since the last red build has, by definition,
// been given the information this guard suppresses. Whatever they did with it —
// re-triggered CI, dragged the card, answered a question — the next failure is
// news to them again, and swallowing it would leave them staring at a card that
// went quiet.
//
// So the reset is only ever WEAKER than the truth, never stronger, and both ways
// of getting it wrong end the hold rather than extending it:
//
//	an unreadable history — no evidence either way. Counting the whole history
//	  instead turns "the store hiccuped" into "nobody has ever touched this
//	  card", which parks tasks people are working on.
//	a window that stops short of the present — BoardEventStore.ListByTask is
//	  `ORDER BY created_at ASC LIMIT n`, so on a task with more events than the
//	  depth this is the OLD end of history. A human touch from ten minutes ago
//	  is simply not in it, and the count it feeds is about rounds already
//	  answered. Provable by the newest event in a full window predating the
//	  pipeline being judged.
//
// reaches is the pipeline's CreatedAt: an event at least that recent is proof
// the window overlaps the period this failure is about. A window that is not
// full cannot have been truncated, so it needs no such proof.
//
// With no event store wired there is no reset at all and the streak is counted
// over the whole pipeline history — the behaviour the guard shipped with, and
// not an uncertainty about the answer.
func (g *PipelineBounceGuard) humanTouchWindow(ctx context.Context, taskID uuid.UUID, reaches time.Time) (time.Time, bool) {
	if g.events == nil {
		return time.Time{}, true
	}
	history, err := g.events.ListByTask(ctx, taskID, reviewLoopHistoryDepth)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).
			Msg("pipeline bounce guard: reading board history for the human-touch reset failed, letting the bounce through")
		return time.Time{}, false
	}
	if len(history) >= reviewLoopHistoryDepth && !historyReaches(history, reaches) {
		log.Warn().Str("task_id", taskID.String()).Int("events", len(history)).
			Msg("pipeline bounce guard: board history window does not reach this pipeline, letting the bounce through")
		return time.Time{}, false
	}
	at, _ := lastHumanEventAt(history)
	return at, true
}

// historyReaches reports whether an oldest-first window extends to at least
// `at`. The newest event it holds is its last one.
func historyReaches(history []domain.BoardEvent, at time.Time) bool {
	if len(history) == 0 {
		return false
	}
	return !history[len(history)-1].CreatedAt.Before(at)
}

// priorSameCommitFailures counts the task's EARLIER failed QA-gate pipelines
// for the same commit, since the last human touch.
//
// Every filter here closes a way the count would lie, and the ones about what
// `failed` MEANS are the load-bearing half: three separate writers stamp that
// status on rows where no provider ever returned a verdict, and counting any of
// them makes a task's genuine FIRST red build look like a repeat — which parks
// it instead of telling the developer.
//
//	the pipeline itself      — it is failed and same-SHA by definition; counting
//	                           it would make every first failure look like a repeat.
//	deploy triggers          — a failed stage/prod deploy is a different event on
//	                           a different question, handled by the incident path.
//	non-verdict rows         — superseded, interrupted, and no-workspace rows.
//	                           See pipelineReachedVerdict.
//	pre-human rows           — a failure a person has already been shown. They
//	                           acted on it; the next one is news again.
//
// Created-at ordering is used rather than "any other row" so a race between two
// resolvers settling two pipelines cannot have each one see the other as its
// predecessor and both hold.
func priorSameCommitFailures(history []domain.TaskPipeline, pipeline domain.TaskPipeline, headSHA string, since time.Time) int {
	n := 0
	for _, other := range history {
		if other.ID == pipeline.ID {
			continue
		}
		if other.Status != domain.PipelineStatusFailed {
			continue
		}
		if isDeployTrigger(other.Trigger) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(other.HeadSHA), headSHA) {
			continue
		}
		if !pipelineReachedVerdict(other) {
			continue
		}
		if other.CreatedAt.After(pipeline.CreatedAt) {
			continue
		}
		if !since.IsZero() && !other.CreatedAt.After(since) {
			continue
		}
		n++
	}
	return n
}

// pipelineReachedVerdict reports whether a `failed` row is a build that RAN and
// went red, as opposed to one of the three bookkeeping shapes that write the
// same status:
//
//   - SupersedePending (note "superseded") fails a pending row when a newer
//     trigger replaces it. Nothing ran and nothing was reported.
//   - FailStaleRunning (note "interrupted") fails every pending/running row at
//     process start, and finishInterrupted does the same on shutdown. Those
//     rows already carry head_sha and may already carry a real provider, so
//     the note is the only thing separating them from a red build.
//   - persistNoWorkspace (provider "none") fails a pipeline that could not
//     start at all — no workspace, no token, "GitHub is not connected".
//
// Anything still unfinished is excluded too: a row with no finished_at has not
// been told anything by a provider yet, so it cannot have bounced the task.
//
// This deliberately does NOT ask "did it record failed job rows". Not every
// path that reaches a real verdict writes jobs (the gate resolver's own
// timeout/CI-unavailable rows are marker jobs at best), so absence of jobs
// proves nothing either way.
func pipelineReachedVerdict(p domain.TaskPipeline) bool {
	if p.FinishedAt == nil {
		return false
	}
	switch strings.TrimSpace(p.Provider) {
	case "", domain.PipelineProviderNone:
		return false
	}
	return !pipelineBookkeepingNote(p.Note)
}

// pipelineBookkeepingNote matches the notes the two "nothing ran" writers
// stamp. Substring, case-insensitive, because the notes are literals today
// ("superseded", "interrupted") but are read back from rows written by older
// binaries.
func pipelineBookkeepingNote(note string) bool {
	lower := strings.ToLower(note)
	return strings.Contains(lower, "supersede") || strings.Contains(lower, "interrupted")
}

// parkTarget re-reads the task and decides what may be done to it.
//
// The task the pipeline job carries is a SNAPSHOT, taken when the pipeline was
// queued. A QA pipeline can easily settle half an hour later, and in that window
// three things happen often enough to have to be handled:
//
//   - a human dragged the card somewhere else. Parking would haul it back into
//     `blocked` out from under them, for a verdict about a column it has left —
//     and COMMENTING there is worse still, because a comment on a dispatchable
//     card starts an agent run. Held, silently, with a warn line naming the card
//     and the column it is in now.
//   - a quota / mobile_device / work_order park already holds it. Overwriting
//     blocked_resource with human_decision strands the task permanently: those
//     three have sweepers that release them, and human_decision has none by
//     design. Their park is left alone, but the explanation still goes on the
//     card — a blocked task dispatches nothing, so saying so is free, and the
//     alternative is a card that is about to be swept back into a loop with no
//     record of why the board stopped.
//   - another pod's guard already parked it on human_decision. That park IS this
//     guard's, so the explanation is already on the card, whichever pod wrote
//     it: held, and silent, which is what keeps the comment to one.
//
// A read error is the fourth case and the only one that gives up the hold
// entirely: re-reading is what stops the park overwriting somebody else's, so
// acting on the stale snapshot would defeat the re-read at exactly the moment it
// matters. The ordinary bounce is the safe outcome there.
//
// With no reader wired the snapshot is all there is, and the guard behaves as it
// did before the re-read existed.
func (g *PipelineBounceGuard) parkTarget(ctx context.Context, repositoryID uuid.UUID, snapshot domain.BoardTask) (domain.BoardTask, bounceAction) {
	if g.reader == nil {
		if parkableTask(snapshot) {
			return snapshot, bouncePark
		}
		return snapshot, heldAction(snapshot)
	}
	fresh, err := g.reader.GetTask(ctx, repositoryID, snapshot.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", snapshot.ID.String()).
			Msg("pipeline bounce guard: re-reading the task before parking failed, letting the bounce through")
		return snapshot, bounceFailOpen
	}
	if !parkableTask(fresh) {
		log.Info().Str("task_id", fresh.ID.String()).Str("column", string(fresh.Column)).
			Str("blocked_resource", fresh.BlockedResource).
			Msg("pipeline bounce guard: the task is already parked, leaving the park alone")
		return fresh, heldAction(fresh)
	}
	if fresh.Column != snapshot.Column {
		log.Warn().Str("task_id", fresh.ID.String()).
			Str("was", string(snapshot.Column)).Str("column", string(fresh.Column)).
			Msg("pipeline bounce guard: the task left the column this pipeline was judging, holding it silently")
		return fresh, bounceSilent
	}
	return fresh, bouncePark
}

// heldAction decides what may be SAID to a task the guard is holding but will
// not park.
//
// The only question is whether a comment can start an agent run. Dispatch drops
// every event on a `blocked` task (isDispatchSuspendedTask), so a comment there
// is inert and worth having; anywhere else task.commented fans out to the
// column's agents and the explanation would be the loop's next lap.
//
// human_decision is the exception among blocked tasks: that park is this
// guard's own (or the review-loop cap's), so the card already carries the
// explanation and repeating it on every later sweep is the spam the re-read
// exists to prevent.
func heldAction(task domain.BoardTask) bounceAction {
	if task.Column != domain.TaskColumnBlocked {
		return bounceSilent
	}
	if strings.EqualFold(strings.TrimSpace(task.BlockedResource), domain.ResourceHumanDecision) {
		return bounceSilent
	}
	return bounceExplain
}

// parkableTask reports whether a task is free to be parked: not already in
// `blocked`, and not already waiting on some other resource.
func parkableTask(task domain.BoardTask) bool {
	return task.Column != domain.TaskColumnBlocked && strings.TrimSpace(task.BlockedResource) == ""
}

// comment states the situation on the card. It names the commit, so a reader
// can check for themselves that nothing new was pushed, and it says plainly
// that the board has stopped rather than leaving a card that simply went quiet.
//
// Posted only AFTER the card is in `blocked` — see the park-first ordering in
// Hold — because the commenter is repository.Service and its task.commented
// event dispatches agents on any card that is still dispatchable.
//
// Once per park, and the park is what makes it once: a second firing re-reads
// the task, finds it parked on human_decision and says nothing, on this pod or
// any other.
func (g *PipelineBounceGuard) comment(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, pipeline domain.TaskPipeline, headSHA string) {
	if g.tasks == nil {
		return
	}
	body := "Pipeline aynı commit için yine kırmızı (" + domain.ShortSHA(headSHA) + ") ve arada YENİ bir commit gelmedi. " +
		"Bu görev bu commit yüzünden zaten bir kez geri gönderildi; sonuç değişmediği için board onu tekrar döngüye sokmayacak — " +
		"kart `blocked` kolonuna alındı.\n\n" +
		"Yapılması gereken bir insanda: CI'ı düzeltin (build hatası, ya da hesap/faturalandırma kaynaklı olarak " +
		"\"job was not started\" diyen bir Actions çalıştırması) ve ardından kartı elle ilerletin. " +
		"Ajan çalıştırmak bu noktada aynı sonucu üretir ve kotayı harcar."
	if note := strings.TrimSpace(pipeline.Note); note != "" {
		body += "\n\nSon pipeline notu: " + truncateTail(note, 500)
	}
	if _, err := g.tasks.AddComment(ctx, repositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    body,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("pipeline bounce guard: the loop-stopped comment failed")
	}
}

// park moves the card to `blocked` on the human-decision resource, keeping the
// column it came from in blocked_origin_column so a human dragging it back out
// lands it where it was. It reports whether the card actually reached `blocked`.
//
// That report is what the caller gates the comment and the hold on. A park that
// did not happen leaves a dispatchable card, and holding it then produces the
// worst of every outcome: the failure report is swallowed, nothing says why, and
// the next board event dispatches an agent onto the same red commit anyway. So a
// failed or unwired park ends the hold and the ordinary bounce goes ahead —
// noisy, but the developer is told and the card keeps moving.
func (g *PipelineBounceGuard) park(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, headSHA string) bool {
	if g.parker == nil {
		return false
	}
	if task.Column == domain.TaskColumnBlocked {
		return false
	}
	detail := "pipeline is still failing for " + domain.ShortSHA(headSHA) +
		" with no new commit — a human has to fix CI or move this task on"
	previous, err := g.parker.BlockOnResource(ctx, repositoryID, task.ID, domain.ResourceHumanDecision, detail)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).
			Msg("pipeline bounce guard: parking the task failed; letting the bounce through instead")
		return false
	}
	// Unlike WorkOrder.Park this one does NOT happen inside Dispatch, so no
	// task.moved event has been written for it by anybody. Without the journal
	// the card would jump to `blocked` with nothing in its history saying when
	// or why — the gap ParkJournal was written to close.
	g.parks.Record(ctx, repositoryID, task, previous,
		domain.ResourceHumanDecision, domain.MoveReasonPipelineLoopParked)
	return true
}
