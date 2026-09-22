package board

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type bounceParker struct {
	resources []string
	details   []string
	previous  domain.TaskColumn
	err       error
	onto      *gateTaskStore
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

func humanTouch(at time.Time) domain.BoardEvent {
	raw, _ := json.Marshal(map[string]interface{}{"author_type": "user"})
	return domain.BoardEvent{
		ID:        uuid.New(),
		EventType: domain.BoardEventTaskCommented,
		Payload:   raw,
		CreatedAt: at,
	}
}

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

type bounceRig struct {
	guard     *PipelineBounceGuard
	parker    *bounceParker
	board     *bounceEventStore
	commenter *bounceCommenter
	events    *parkEventStore
	spans     *parkSpanStore
}

func withBounceGuard(h *gateHarness) *bounceRig {
	return withBounceGuardHistory(h, nil)
}

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

func holdOnRepeatFailure(t *testing.T, h *gateHarness, rig *bounceRig) bool {
	t.Helper()
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	snapshot := domain.BoardTask{
		ID: h.tasks.task.ID, RepositoryID: h.repoID, Key: h.tasks.task.Key,
		Column: domain.TaskColumnCodeReview,
	}
	return rig.guard.Hold(context.Background(), h.repoID, snapshot, h.pipeline)
}

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

func stubRedBuild(t *testing.T) {
	t.Helper()
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "completed", Conclusion: "failure"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "completed", Conclusion: "failure"}})
}

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

func TestAFailureOnAnAlreadyParkedTaskIsHeldSilently(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuard(h)
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

func TestPipelinesThatNeverReachedAVerdictAreNotPriorFailures(t *testing.T) {
	cases := []struct {
		name       string
		row        domain.TaskPipeline
		unfinished bool
	}{
		{
			name: "superseded by a newer trigger",
			row:  domain.TaskPipeline{Provider: domain.PipelineProviderGitHubActions, Note: "superseded"},
		},
		{
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

func TestFailureAfterAHumanTouchBouncesAgain(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	seedFailedPipeline(t, h, h.pipeline.HeadSHA, time.Now().Add(-30*time.Minute))
	rig := withBounceGuardHistory(h, []domain.BoardEvent{humanTouch(time.Now().Add(-20 * time.Minute))})
	stubRedBuild(t)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	assertOrdinaryBounce(t, h, rig, "a human has looked at the card since that failure")
}

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

func TestATaskThatLeftTheColumnIsHeldSilently(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	rig := withBounceGuardHistory(h, nil)
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
	if len(rig.commenter.contents) != 0 {
		t.Errorf("commented on a dispatchable card in another column: %v", rig.commenter.contents)
	}
	if len(rig.events.all()) != 0 {
		t.Errorf("journalled a park that did not happen: %v", rig.events.payloads())
	}
}

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

func TestAPipelineWithNoHeadSHAIsNeverHeld(t *testing.T) {
	guard := NewPipelineBounceGuard(newFakePipelineStore(), nil, nil)
	held := guard.Hold(context.Background(), uuid.New(), domain.BoardTask{ID: uuid.New()},
		domain.TaskPipeline{ID: uuid.New(), Status: domain.PipelineStatusFailed})
	if held {
		t.Fatal("held a pipeline that cannot say which commit it was about")
	}
}

func TestNilBounceGuardHoldsNothing(t *testing.T) {
	var guard *PipelineBounceGuard
	if guard.Hold(context.Background(), uuid.New(), domain.BoardTask{}, domain.TaskPipeline{HeadSHA: "abc"}) {
		t.Fatal("a nil guard held a bounce")
	}
}
