package board

// The same-commit re-bounce brake.
//
// Reproduced from the production loop it was written for: the org's GitHub
// Actions were billing-blocked, so every check failed instantly with no commit
// ever changing, and the board sent task B-4 back to the developer eleven times
// in forty minutes — ~40 agent runs, all on the user's paid quota, all about
// one commit nobody had touched since the first failure.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// bounceParker is the board task store's park, remembered — and applied. The
// real BlockOnResource writes the column and the resource onto the row, which
// is what the guard's own re-read finds on its next firing, so a fake that only
// recorded the call could never exercise the idempotency that re-read buys.
type bounceParker struct {
	resources []string
	details   []string
	previous  domain.TaskColumn
	err       error
	// onto is the task the park is applied to, so a later GetTask sees it.
	onto *gateTaskStore
}

func (b *bounceParker) BlockOnResource(_ context.Context, _, _ uuid.UUID, resource, detail string) (domain.TaskColumn, error) {
	if b.err != nil {
		return "", b.err
	}
	b.resources = append(b.resources, resource)
	b.details = append(b.details, detail)
	if b.onto != nil {
		b.previous = b.onto.task.Column
		b.onto.task.BlockedOriginColumn = b.onto.task.Column
		b.onto.task.Column = domain.TaskColumnBlocked
		b.onto.task.BlockedResource = resource
	}
	return b.previous, nil
}

// bounceEventStore is the board history the human-touch reset reads.
type bounceEventStore struct {
	events []domain.BoardEvent
	limits []int
	err    error
}

func (s *bounceEventStore) ListByTask(_ context.Context, _ uuid.UUID, limit int) ([]domain.BoardEvent, error) {
	s.limits = append(s.limits, limit)
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

// humanTouch is the cheapest human event there is: somebody typed on the card.
func humanTouch(at time.Time) domain.BoardEvent {
	raw, _ := json.Marshal(map[string]interface{}{"author_type": "user"})
	return domain.BoardEvent{
		ID:        uuid.New(),
		EventType: domain.BoardEventTaskCommented,
		Payload:   raw,
		CreatedAt: at,
	}
}

// bounceCommenter is the guard's commenter, which in production is
// repository.Service: AddComment emits task.commented, Dispatch resolves the
// column's agents for it and enqueues a run. That is why WHEN it is called
// matters, so this records the card's column and blocked resource as of every
// comment — a comment written while the card was still in code_review would
// have started the agent run the guard exists to prevent.
type bounceCommenter struct {
	tasks    *gateTaskStore
	contents []string
	columns  []domain.TaskColumn
	blocked  []string
}

func (c *bounceCommenter) AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	c.contents = append(c.contents, req.Content)
	c.columns = append(c.columns, c.tasks.task.Column)
	c.blocked = append(c.blocked, c.tasks.task.BlockedResource)
	return c.tasks.AddComment(ctx, repositoryID, taskID, req)
}

// bounceRig is the wired guard and the collaborators a test asserts on.
type bounceRig struct {
	guard     *PipelineBounceGuard
	parker    *bounceParker
	board     *bounceEventStore
	commenter *bounceCommenter
	events    *parkEventStore
	spans     *parkSpanStore
}

// withBounceGuard installs a fully wired guard on the harness's runner and
// hands back the collaborators a test asserts on.
func withBounceGuard(h *gateHarness) *bounceRig {
	return withBounceGuardHistory(h, nil)
}

// withBounceGuardHistory is the same wiring plus the board-event history the
// human-touch reset reads.
func withBounceGuardHistory(h *gateHarness, history []domain.BoardEvent) *bounceRig {
	rig := &bounceRig{
		parker:    &bounceParker{previous: domain.TaskColumnCodeReview, onto: h.tasks},
		board:     &bounceEventStore{events: history},
		commenter: &bounceCommenter{tasks: h.tasks},
		events:    &parkEventStore{},
		spans:     &parkSpanStore{},
	}
	rig.guard = NewPipelineBounceGuard(h.store, rig.commenter, rig.parker)
	rig.guard.SetParkJournal(NewParkJournal(rig.events, rig.spans))
	rig.guard.SetEventHistory(rig.board)
	rig.guard.SetTaskReader(h.tasks)
	h.runner.SetBounceGuard(rig.guard)
	return rig
}

// holdOnRepeatFailure asks the guard the question PipelineRunner asks it on a
// red build — with the task as it looked when the pipeline was QUEUED, one
// earlier failure for the same commit already on the record.
//
// Called directly rather than through ResolveUnfinished because the resolver
// re-reads the task first and settles a pipeline quietly the moment the card is
// not in code_review: every case about a card that MOVED while the pipeline was
// hanging is a case the runner never lets the guard see, so driving them through
// it asserts nothing about the guard at all.
func holdOnRepeatFailure(t *testing.T, h *gateHarness, rig *bounceRig) bool {
	t.Helper()
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	snapshot := domain.BoardTask{
		ID: h.tasks.task.ID, RepositoryID: h.repoID, Key: h.tasks.task.Key,
		Column: domain.TaskColumnCodeReview,
	}
	return rig.guard.Hold(context.Background(), h.repoID, snapshot, h.pipeline)
}

// filledWindow is reviewLoopHistoryDepth events, i.e. exactly the ceiling
// ListByTask returns — the shape that proves nothing about what came after it.
func filledWindow(at time.Time) []domain.BoardEvent {
	out := make([]domain.BoardEvent, 0, 500)
	for i := 0; i < 500; i++ {
		out = append(out, domain.BoardEvent{
			ID:        uuid.New(),
			EventType: domain.BoardEventTaskMoved,
			CreatedAt: at,
		})
	}
	return out
}

// seedFailedPipeline writes an already-settled failed pipeline for the same
// task: a build that RAN, reported to a provider and went red. That is the only
// shape that counts as a lap the board has already done — see
// seedNonVerdictPipeline for the three that write `failed` without it.
func seedFailedPipeline(t *testing.T, h *gateHarness, headSHA string, createdAt time.Time) domain.TaskPipeline {
	t.Helper()
	return seedPipelineRow(t, h, domain.TaskPipeline{
		TaskID:       h.tasks.task.ID,
		RepositoryID: h.repoID,
		Trigger:      domain.PipelineTriggerReadyForQA,
		Status:       domain.PipelineStatusFailed,
		Provider:     domain.PipelineProviderGitHubActions,
		HeadSHA:      headSHA,
	}, createdAt, &createdAt)
}

// seedPipelineRow creates a pipeline and back-dates it, since the fake store
// keeps whatever CreatedAt it is handed.
func seedPipelineRow(t *testing.T, h *gateHarness, row domain.TaskPipeline, createdAt time.Time, finishedAt *time.Time) domain.TaskPipeline {
	t.Helper()
	created, err := h.store.Create(context.Background(), row)
	if err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	created.CreatedAt = createdAt
	created.FinishedAt = finishedAt
	if _, err := h.store.Update(context.Background(), created); err != nil {
		t.Fatalf("age pipeline: %v", err)
	}
	return created
}

// stubRedBuild makes GitHub report the billing-block shape: the run completed,
// the mapped job did not succeed, and no commit is involved in the answer.
func stubRedBuild(t *testing.T) {
	t.Helper()
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "completed", Conclusion: "failure"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "completed", Conclusion: "failure"}})
}

// assertOrdinaryBounce is the shape of "the guard stayed out of it": the task
// went back to the developer and nothing was parked.
func assertOrdinaryBounce(t *testing.T, h *gateHarness, rig *bounceRig, why string) {
	t.Helper()
	if len(h.tasks.moves) != 1 || h.tasks.moves[0] != domain.TaskColumnNeedRevision {
		t.Fatalf("moves = %v, want one move to need_revision: %s", h.tasks.moves, why)
	}
	if len(rig.parker.resources) != 0 {
		t.Errorf("the task was parked (%v): %s", rig.parker.resources, why)
	}
	if len(rig.commenter.contents) != 0 {
		t.Errorf("the guard commented on a bounce it stayed out of (%v): %s", rig.commenter.contents, why)
	}
}

// The first red build on a commit is news, and news goes back to the developer.
// The guard must be invisible here or it would swallow every real failure.
func TestFirstFailureOnACommitStillBouncesTheTask(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuard(h)
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	assertOrdinaryBounce(t, h, rig, "a first failure is news")
	if h.tasks.reasons[0] != domain.MoveReasonPipelineFailed {
		t.Errorf("system reason = %q, want %q", h.tasks.reasons[0], domain.MoveReasonPipelineFailed)
	}
	if len(rig.events.all()) != 0 {
		t.Errorf("a park was journalled for a first failure: %v", rig.events.payloads())
	}
}

// The lap that must not happen. The identical commit fails again, so there is
// nothing to tell the developer that the card does not already say: no move, no
// re-dispatch, one explanation, and the card parked for a person.
func TestSameCommitFailingAgainParksInsteadOfBouncing(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuard(h)
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if settled := h.settled(t); settled.Status != domain.PipelineStatusFailed {
		t.Errorf("status = %q, want failed: the row must still record what CI said", settled.Status)
	}
	if len(h.tasks.moves) != 0 {
		t.Fatalf("the task was moved again for a commit it was already sent back for: %v", h.tasks.moves)
	}
	if len(h.qa.calls) != 0 {
		t.Fatalf("a reviewer was dispatched onto a red build: %v", h.qa.calls)
	}
	if len(h.tasks.comments) != 1 {
		t.Fatalf("got %d comments, want exactly 1 — the guard explains itself once and never re-reports the build",
			len(h.tasks.comments))
	}
	comment := h.tasks.comments[0]
	for _, want := range []string{domain.ShortSHA(h.pipeline.HeadSHA), "blocked"} {
		if !strings.Contains(comment, want) {
			t.Errorf("the comment does not mention %q, so nobody can check it: %s", want, comment)
		}
	}

	if len(rig.parker.resources) != 1 || rig.parker.resources[0] != domain.ResourceHumanDecision {
		t.Fatalf("parked on %v, want one park on %q", rig.parker.resources, domain.ResourceHumanDecision)
	}
	// The origin column has to survive the park, or a human dragging the card
	// back out lands it in whatever the store's fallback is.
	payloads := rig.events.payloads()
	if len(payloads) != 1 {
		t.Fatalf("got %d park events, want 1: a card that jumps to blocked with no history is the gap ParkJournal closes",
			len(payloads))
	}
	if payloads[0]["from_column"] != string(domain.TaskColumnCodeReview) {
		t.Errorf("from_column = %v, want code_review — the pre-park column the store reported",
			payloads[0]["from_column"])
	}
	if payloads[0]["to_column"] != string(domain.TaskColumnBlocked) {
		t.Errorf("to_column = %v, want blocked", payloads[0]["to_column"])
	}
	if payloads[0][domain.EventPayloadReason] != domain.MoveReasonPipelineLoopParked {
		t.Errorf("system_reason = %v, want %q", payloads[0][domain.EventPayloadReason], domain.MoveReasonPipelineLoopParked)
	}
	if moves := rig.spans.recorded(); len(moves) != 1 || moves[0].column != string(domain.TaskColumnBlocked) {
		t.Errorf("column spans = %v, want the visit closed into blocked", moves)
	}
}

// Second firing on a card that is already parked: still held, still unmoved,
// and SILENT. The idempotency is read off the task, not off an arithmetic
// coincidence — the re-read is what makes it hold across pods, where the first
// park may well have been written by a different process.
func TestAFailureOnAnAlreadyParkedTaskIsHeldSilently(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuard(h)
	// The pipeline was queued on a card in code_review; by the time it settles,
	// somebody's guard — this pod's or another's — has already parked it.
	h.tasks.task.Column = domain.TaskColumnBlocked
	h.tasks.task.BlockedResource = domain.ResourceHumanDecision

	if !holdOnRepeatFailure(t, h, rig) {
		t.Fatal("let a third identical failure through onto a parked card")
	}

	if len(h.tasks.moves) != 0 {
		t.Fatalf("the task was moved on its third identical failure: %v", h.tasks.moves)
	}
	if len(rig.commenter.contents) != 0 {
		t.Fatalf("got %d comments, want 0 — the explanation is already on the card: %v",
			len(rig.commenter.contents), rig.commenter.contents)
	}
	if len(rig.parker.resources) != 0 {
		t.Errorf("re-parked a task that is already parked: %v", rig.parker.resources)
	}
}

// A NEW commit is new information, whatever happened to the old one. The guard
// is per head SHA precisely so a developer who pushed a fix still gets told
// their fix did not work.
func TestFailureOnANewCommitBouncesEvenAfterAnEarlierOneFailed(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuard(h)
	seedFailedPipeline(t, h, "0000000000000000000000000000000000000000", time.Now().Add(-30*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	assertOrdinaryBounce(t, h, rig, "a commit that has not failed before")
}

// The three writers that stamp `failed` on a row where nothing ever ran. Each
// one of them, counted, turns a task's genuine FIRST red build into a park —
// and the interrupted case is not hypothetical: PipelineRunner.Start calls
// FailStaleRunning(0) at every process boot, so a single restart used to be
// enough to arm this against the next real failure.
func TestPipelinesThatNeverReachedAVerdictAreNotPriorFailures(t *testing.T) {
	cases := []struct {
		name string
		row  domain.TaskPipeline
		// unfinished leaves finished_at nil.
		unfinished bool
	}{
		{
			name: "superseded by a newer trigger",
			row:  domain.TaskPipeline{Provider: domain.PipelineProviderGitHubActions, Note: "superseded"},
		},
		{
			// FailStaleRunning at process start, and finishInterrupted on
			// shutdown. Both keep whatever provider was already stamped, so the
			// note is the only thing telling them apart from a red build.
			name: "interrupted by a restart",
			row:  domain.TaskPipeline{Provider: domain.PipelineProviderGitHubActions, Note: "interrupted"},
		},
		{
			name: "never started: GitHub is not connected",
			row:  domain.TaskPipeline{Provider: domain.PipelineProviderNone, Note: "GitHub is not connected"},
		},
		{
			name: "no provider recorded at all",
			row:  domain.TaskPipeline{Note: "could not resolve git info"},
		},
		{
			name:       "still unfinished, so nothing has reported",
			row:        domain.TaskPipeline{Provider: domain.PipelineProviderGitHubActions},
			unfinished: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
			rig := withBounceGuard(h)
			createdAt := time.Now().Add(-30 * time.Minute)
			row := tc.row
			row.TaskID = h.tasks.task.ID
			row.RepositoryID = h.repoID
			row.Trigger = domain.PipelineTriggerReadyForQA
			row.Status = domain.PipelineStatusFailed
			row.HeadSHA = h.pipeline.HeadSHA
			finishedAt := &createdAt
			if tc.unfinished {
				finishedAt = nil
			}
			seedPipelineRow(t, h, row, createdAt, finishedAt)
			stubRedBuild(t)

			if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
				t.Fatalf("resolve: %v", err)
			}

			assertOrdinaryBounce(t, h, rig, "nothing ran for the seeded row, so this is the first genuine failure")
		})
	}
}

// A person who has been shown the red build gets told about the next one. They
// have the information the guard suppresses; whatever they did with it, the
// following failure is news to them again — and a card that silently stops
// after a human touched it is exactly the "work just stopped" report the guard
// was written to avoid producing.
func TestFailureAfterAHumanTouchBouncesAgain(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	// The failure the human saw, then the human.
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	rig := withBounceGuardHistory(h, []domain.BoardEvent{humanTouch(time.Now().Add(-20 * time.Minute))})
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	assertOrdinaryBounce(t, h, rig, "a human has looked at the card since that failure")
}

// The reset is about failures the human has ALREADY seen. One that landed after
// they walked away still counts, or the streak would never restart.
func TestFailureAfterAHumanTouchStillCountsLaterFailures(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, []domain.BoardEvent{humanTouch(time.Now().Add(-30 * time.Minute))})
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-20*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(h.tasks.moves) != 0 {
		t.Fatalf("bounced on a repeat failure that postdates the human touch: %v", h.tasks.moves)
	}
	if len(rig.parker.resources) != 1 {
		t.Errorf("parked %v, want one park: the failure came after the human, so they have not seen it", rig.parker.resources)
	}
}

// The human-touch reset is the only thing that can say "a person has already
// been told". An unreadable board history is not evidence they have NOT been —
// counting the whole history there parks tasks people are working on, on the
// strength of a store hiccup — so the guard steps aside and the failure is
// reported the ordinary way.
func TestUnreadableBoardHistoryLetsTheBounceThrough(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, nil)
	rig.board.err = errors.New("board_events is down")
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	assertOrdinaryBounce(t, h, rig, "an unreadable history proves nothing about who has seen what")
}

// BoardEventStore.ListByTask is `ORDER BY created_at ASC LIMIT n`, so a full
// window is the OLD end of a busy task's history. A human touch from ten minutes
// ago is not in it, and holding on that window suppresses a failure the person
// working the card has never seen.
func TestAHistoryWindowThatDoesNotReachThePresentLetsTheBounceThrough(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, filledWindow(time.Now().Add(-3*time.Hour)))
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	assertOrdinaryBounce(t, h, rig, "the window stops three hours short of the pipeline being judged")
}

// …and the check is about REACH, not about size. A full window whose newest
// event postdates the pipeline has seen everything that matters, so the brake
// still works on the busiest task on the board.
func TestAFullHistoryWindowThatReachesThePresentStillParks(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, filledWindow(time.Now()))
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(h.tasks.moves) != 0 {
		t.Fatalf("bounced with a window that covers the whole question: %v", h.tasks.moves)
	}
	if len(rig.parker.resources) != 1 {
		t.Errorf("parked %v, want one park", rig.parker.resources)
	}
}

// The re-read is the ONLY thing standing between this park and somebody else's
// (a quota park it would strand, a column a human moved the card to). A task the
// guard could not re-read is one it knows nothing about, so it does not act at
// all — the stale snapshot is exactly what the re-read exists to distrust.
func TestATaskThatCannotBeReReadLetsTheBounceThrough(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, nil)
	h.tasks.getErr = errors.New("tasks table is down")

	if holdOnRepeatFailure(t, h, rig) {
		t.Fatal("held on a task whose current state it could not read")
	}

	if len(rig.parker.resources) != 0 {
		t.Errorf("parked on the strength of a stale snapshot: %v", rig.parker.resources)
	}
	if len(rig.commenter.contents) != 0 {
		t.Errorf("commented on a card it could not read: %v", rig.commenter.contents)
	}
	if len(rig.events.all()) != 0 {
		t.Errorf("journalled a park that did not happen: %v", rig.events.payloads())
	}
}

// The park is what makes the comment safe, so a park that did not happen means
// no comment AND no hold: the commenter is repository.Service, and a comment on
// a card still sitting in code_review dispatches an agent onto the red commit.
// Holding without parking would swallow the failure report and stop nothing.
func TestAFailedParkLetsTheBounceThrough(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, nil)
	rig.parker.err = errors.New("tasks table is down")
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	assertOrdinaryBounce(t, h, rig, "nothing parked the card, so nothing is holding it")
	if len(rig.events.all()) != 0 {
		t.Errorf("journalled a park that failed: %v", rig.events.payloads())
	}
}

// The ordering the whole guard turns on.
//
// The commenter is repository.Service: AddComment → emit → Dispatch →
// resolveAgents, i.e. a comment is a dispatch trigger. Commenting first put an
// agent run on a task still in code_review — the guard starting the loop it
// exists to stop. Parking first makes the same comment inert, because Dispatch
// suspends every event on a `blocked` task.
func TestTheParkHappensBeforeTheComment(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, nil)
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(rig.commenter.contents) != 1 {
		t.Fatalf("got %d guard comments, want 1: %v", len(rig.commenter.contents), rig.commenter.contents)
	}
	if rig.commenter.columns[0] != domain.TaskColumnBlocked {
		t.Errorf("the card was in %q when the guard commented, want blocked — that comment dispatches an agent",
			rig.commenter.columns[0])
	}
	if rig.commenter.blocked[0] != domain.ResourceHumanDecision {
		t.Errorf("blocked_resource at comment time = %q, want %q",
			rig.commenter.blocked[0], domain.ResourceHumanDecision)
	}
}

// The snapshot on a pipeline job is from when it was QUEUED. A pipeline that
// settles half an hour later must not drag a card a human has since moved back
// into `blocked` for a verdict about a column it has left.
func TestATaskThatLeftTheColumnIsHeldSilently(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, nil)
	// The pipeline was queued on a card in code_review; a human has dragged it
	// on since. The guard still gets handed the old snapshot.
	h.tasks.task.Column = domain.TaskColumnInProgress

	if !holdOnRepeatFailure(t, h, rig) {
		t.Fatal("re-reported a verdict about a column the card has left")
	}

	if len(h.tasks.moves) != 0 {
		t.Fatalf("moved a task that had already left the column: %v", h.tasks.moves)
	}
	if len(rig.parker.resources) != 0 {
		t.Errorf("parked a task a human had moved on: %v", rig.parker.resources)
	}
	// Silence is the point: in_progress is a DISPATCHABLE column, so a comment
	// here would emit task.commented and put an agent on the card.
	if len(rig.commenter.contents) != 0 {
		t.Errorf("commented on a dispatchable card in another column: %v", rig.commenter.contents)
	}
	if len(rig.events.all()) != 0 {
		t.Errorf("journalled a park that did not happen: %v", rig.events.payloads())
	}
}

// human_decision is the one park no sweeper releases. Overwriting a quota,
// device or work-order park with it strands the task forever, so somebody
// else's park is never touched — but the card still gets told why the board
// stopped, because a quota park IS swept back into circulation and the next lap
// would start with no record of what happened. A blocked card dispatches
// nothing, so that comment cannot start a run.
func TestATaskParkedOnAnotherResourceKeepsItsParkAndGetsTheExplanation(t *testing.T) {
	for _, resource := range []string{
		domain.ResourceClaudeCodeQuota, domain.ResourceMobileDevice, domain.ResourceWorkOrder,
	} {
		t.Run(resource, func(t *testing.T) {
			h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
			rig := withBounceGuardHistory(h, nil)
			h.tasks.task.Column = domain.TaskColumnBlocked
			h.tasks.task.BlockedResource = resource

			if !holdOnRepeatFailure(t, h, rig) {
				t.Fatal("bounced a card that is parked on " + resource)
			}

			if len(h.tasks.moves) != 0 {
				t.Fatalf("moved a task parked on %s: %v", resource, h.tasks.moves)
			}
			if len(rig.parker.resources) != 0 {
				t.Errorf("overwrote a %s park with %v — nothing would ever release it", resource, rig.parker.resources)
			}
			if len(rig.commenter.contents) != 1 {
				t.Fatalf("got %d comments, want 1 saying why the board stopped: %v",
					len(rig.commenter.contents), rig.commenter.contents)
			}
			if rig.commenter.columns[0] != domain.TaskColumnBlocked {
				t.Errorf("commented on a card in %q — only `blocked` suspends dispatch", rig.commenter.columns[0])
			}
		})
	}
}

// A pipeline that never recorded its commit cannot be shown to repeat one, and
// the guard must fail OPEN there: suppressing a failure nobody proved was a
// repeat would silently swallow real red builds.
func TestAPipelineWithNoHeadSHAIsNeverHeld(t *testing.T) {
	guard := NewPipelineBounceGuard(newFakePipelineStore(), nil, nil)
	held := guard.Hold(context.Background(), uuid.New(), domain.BoardTask{ID: uuid.New()},
		domain.TaskPipeline{ID: uuid.New(), Status: domain.PipelineStatusFailed})
	if held {
		t.Fatal("held a pipeline that cannot say which commit it was about")
	}
}

// An unwired guard is the old behaviour, exactly.
func TestNilBounceGuardHoldsNothing(t *testing.T) {
	var guard *PipelineBounceGuard
	if guard.Hold(context.Background(), uuid.New(), domain.BoardTask{}, domain.TaskPipeline{HeadSHA: "abc"}) {
		t.Fatal("a nil guard held a bounce")
	}
}
