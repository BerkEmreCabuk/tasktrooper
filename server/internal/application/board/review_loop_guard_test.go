package board_test

// The general review-cycle cap.
//
// The second half of the B-4 loop, and the half no pipeline appears in: QA sent
// the task to need_revision, the developer run found nothing to change, the
// automatic hand-off put it straight back into code_review, and round it went.
// Nothing in that cycle is wrong on its own — which is exactly why the brake
// counts the SHAPE (arrivals in need_revision with no human in between) rather
// than trying to diagnose a cause.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// loopParker is the board task store's park, remembered.
type loopParker struct {
	resources []string
	details   []string
	err       error
}

func (p *loopParker) BlockOnResource(_ context.Context, _, _ uuid.UUID, resource, detail string) (domain.TaskColumn, error) {
	if p.err != nil {
		return "", p.err
	}
	p.resources = append(p.resources, resource)
	p.details = append(p.details, detail)
	return domain.TaskColumnNeedRevision, nil
}

// loopCommenter is the task comment store, remembered — with whether the card
// had already been parked at the moment of each comment.
//
// That ordering is the whole safety of the comment. In production the commenter
// is repository.Service: AddComment emits task.commented, which re-enters
// Dispatch and fans out to the column's agents. On a card still sitting in
// need_revision that comment starts the developer run the cap just refused to
// start; on one already in `blocked`, isDispatchSuspendedTask drops it.
type loopCommenter struct {
	parker   *loopParker
	contents []string
	parked   []bool
}

func (c *loopCommenter) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	c.contents = append(c.contents, req.Content)
	c.parked = append(c.parked, c.parker != nil && len(c.parker.resources) > 0)
	return domain.TaskComment{}, nil
}

// installReviewLoopGuard wires the cap onto the suite's dispatcher, exactly as
// runtime does — journal included, because the park is a move OUT of
// need_revision that nothing else records.
func (s *DispatcherSuite) installReviewLoopGuard() (*loopParker, *loopCommenter, *fakeSpanStore) {
	parker := &loopParker{}
	commenter := &loopCommenter{parker: parker}
	spans := &fakeSpanStore{}
	guard := board.NewReviewLoopGuard(s.events, parker)
	guard.SetCommenter(commenter)
	guard.SetParkJournal(board.NewParkJournal(s.events, spans))
	s.disp.SetReviewLoopGuard(guard)
	return parker, commenter, spans
}

// seedSyntheticMove writes the shape Dispatch stamps when NOBODY moved
// anything: the reconciler reviving an idle task, or a sweeper waking a parked
// one. Both carry Dispatch's `column` key and no to_column at all, so counting
// them would read the board's own housekeeping as another revision lap.
func (s *DispatcherSuite) seedSyntheticMove(taskID uuid.UUID, column domain.TaskColumn, extra map[string]interface{}) {
	payload := map[string]interface{}{
		"column":                 string(column),
		domain.EventPayloadActor: domain.EventActorSystem,
	}
	for k, v := range extra {
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	s.Require().NoError(err)
	_, err = s.events.Create(context.Background(), domain.BoardEvent{
		TaskID:    taskID,
		EventType: domain.BoardEventTaskMoved,
		Payload:   raw,
	})
	s.Require().NoError(err)
}

// seedMove writes the board event a completed move leaves behind, in the shape
// repository.Service actually writes it (from/to plus the actor).
func (s *DispatcherSuite) seedMove(taskID uuid.UUID, from, to domain.TaskColumn, actor string) {
	raw, err := json.Marshal(map[string]interface{}{
		"from_column":            string(from),
		"to_column":              string(to),
		"column":                 string(to),
		domain.EventPayloadActor: actor,
	})
	s.Require().NoError(err)
	_, err = s.events.Create(context.Background(), domain.BoardEvent{
		TaskID:    taskID,
		EventType: domain.BoardEventTaskMoved,
		Payload:   raw,
	})
	s.Require().NoError(err)
}

// seedHumanComment writes the cheapest possible human touch: somebody typed on
// the card. Any human event at all is meant to reset the count.
func (s *DispatcherSuite) seedHumanComment(taskID uuid.UUID) {
	raw, err := json.Marshal(map[string]interface{}{"author_type": "user"})
	s.Require().NoError(err)
	_, err = s.events.Create(context.Background(), domain.BoardEvent{
		TaskID:    taskID,
		EventType: domain.BoardEventTaskCommented,
		Payload:   raw,
	})
	s.Require().NoError(err)
}

// dispatchIntoNeedRevision replays a reviewer sending the task back.
func (s *DispatcherSuite) dispatchIntoNeedRevision(repositoryID, taskID uuid.UUID, assignee uuid.UUID) error {
	return s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID: taskID, RepositoryID: repositoryID, Key: "B-4", Title: "loop",
			Column: domain.TaskColumnNeedRevision, AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"from_column":            string(domain.TaskColumnCodeReview),
			"to_column":              string(domain.TaskColumnNeedRevision),
			domain.EventPayloadActor: domain.EventActorAgent,
		},
	})
}

// Two laps are review working. The developer must still be dispatched.
func (s *DispatcherSuite) TestSecondNeedRevisionEntryStillDispatchesTheDeveloper() {
	parker, commenter, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Len(s.runs.runs, 1, "a second revision round is ordinary review, not a loop")
	s.Len(s.runner.jobs, 1)
	s.Empty(parker.resources)
	s.Empty(commenter.contents)
}

// The third arrival with nobody having looked is the brake.
func (s *DispatcherSuite) TestThirdNeedRevisionEntryWithoutHumanInputParksTheTask() {
	parker, commenter, spans := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnInProgress, domain.TaskColumnCodeReview, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorSystem)

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Empty(s.runs.runs, "the developer was dispatched onto a task the board has already circulated three times")
	s.Empty(s.runner.jobs)
	s.Require().Len(s.events.events, 5,
		"board history records the move that reached the guard, and the park that took the card back out")

	s.Require().Len(parker.resources, 1)
	s.Equal(domain.ResourceHumanDecision, parker.resources[0],
		"parked on a resource some sweeper would release, which would restart the loop")
	s.Contains(parker.details[0], "review loop")

	s.Require().Len(commenter.contents, 1, "one comment, saying what stopped and why")
	s.Contains(commenter.contents[0], "review loop: sent back 3 times without human input")
	s.True(strings.Contains(commenter.contents[0], "blocked"), "the comment must say where the card went")

	// The park is need_revision → blocked, a SECOND move from the one Dispatch
	// wrote to get the card here. Unjournalled, the card jumps to blocked with
	// nothing saying why and the need_revision span never closes — a task
	// parked for two days reads as two days of active revision.
	var park map[string]interface{}
	s.Require().NoError(json.Unmarshal(s.events.events[4].Payload, &park))
	s.Equal(string(domain.TaskColumnNeedRevision), park["from_column"])
	s.Equal(string(domain.TaskColumnBlocked), park["to_column"])
	s.Equal(domain.MoveReasonReviewLoopParked, park[domain.EventPayloadReason],
		"the park reason the UI renders — dead until something journals it")
	s.Equal(domain.ResourceHumanDecision, park["resource"])
	s.Equal([]string{taskID.String() + ":" + string(domain.TaskColumnBlocked)}, spans.moves,
		"the need_revision span stays open for the whole park without this")
}

// The board's own housekeeping is not a revision lap. The reconciler re-dispatching
// an idle task and the sweepers waking a parked one all go through Dispatch, which
// stamps `column` on every event it writes — so a need_revision task whose dev run
// keeps failing used to be parked after three reconciler sweeps without ever having
// looped once.
func (s *DispatcherSuite) TestSyntheticMovesAreNotCountedAsRevisionLaps() {
	for name, extra := range map[string]map[string]interface{}{
		"reconciler re-dispatch": {"reconciled": true, "reason": "retry_failed_run"},
		"quota sweeper resume":   {"resumed": "quota_reset", "resource": domain.ResourceClaudeCodeQuota},
		"work order sweeper":     {"resumed": "work_order_clear", domain.EventPayloadResumedResource: domain.ResourceWorkOrder},
		"deploy sweeper":         {"resumed": "deploy_settled", domain.EventPayloadResumedResource: domain.ResourceDeployWatch},
	} {
		s.Run(name, func() {
			s.SetupTest()
			parker, commenter, _ := s.installReviewLoopGuard()
			taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
			for i := 0; i < 5; i++ {
				s.seedSyntheticMove(taskID, domain.TaskColumnNeedRevision, extra)
			}

			s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

			s.Len(s.runner.jobs, 1, "five sweeps of the board's own housekeeping are not five revision rounds")
			s.Empty(parker.resources)
			s.Empty(commenter.contents)
		})
	}
}

// A move event that only carries Dispatch's `column` stamp says nothing about
// where the task came FROM, so it cannot be shown to be an arrival. Only
// repository.Service's to_column/from_column pair can.
func (s *DispatcherSuite) TestColumnStampAloneIsNotAnArrival() {
	parker, _, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	for i := 0; i < 5; i++ {
		s.seedSyntheticMove(taskID, domain.TaskColumnNeedRevision, nil)
	}

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Len(s.runner.jobs, 1)
	s.Empty(parker.resources)
}

// ListByTask is oldest-first WITH a limit, so past the depth the window is the
// wrong end of history: old rounds nobody is repeating today. Not finding the
// move that reached the guard is exactly that condition, and it fails open.
func (s *DispatcherSuite) TestHistoryLongerThanTheWindowFailsOpen() {
	parker, commenter, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	for i := 0; i < 501; i++ {
		s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	}

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Len(s.runner.jobs, 1,
		"501 ancient rounds are not evidence about today; the window cannot see this move at all")
	s.Empty(parker.resources)
	s.Empty(commenter.contents)
}

// A caller with no event id cannot prove the window reaches the present either.
// uuid.Nil used to SKIP the check and count anyway, which is the same bug with
// an extra step.
func (s *DispatcherSuite) TestNilCurrentEventIDFailsOpen() {
	parker := &loopParker{}
	taskID := uuid.New()
	guard := board.NewReviewLoopGuard(s.events, parker)
	for i := 0; i < 5; i++ {
		s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	}

	held := guard.Hold(context.Background(), uuid.New(),
		domain.BoardTask{ID: taskID, Column: domain.TaskColumnNeedRevision}, uuid.Nil)

	s.False(held, "parked on a window nothing proved reaches the present")
	s.Empty(parker.resources)
}

// Any human touch resets the count: a person actively working a hard task may
// send it back as many times as they like. Only the unattended board is capped.
func (s *DispatcherSuite) TestHumanEventResetsTheReviewLoopCount() {
	parker, commenter, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	// …and then somebody looked at it.
	s.seedHumanComment(taskID)

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Len(s.runs.runs, 1, "the rounds before a human intervened are ones they have already seen")
	s.Len(s.runner.jobs, 1)
	s.Empty(parker.resources)
	s.Empty(commenter.contents)
}

// A human MOVE resets it too — the uid Dispatch stamps on the event row is the
// other half of "a person was involved", and it arrives without the actor key.
func (s *DispatcherSuite) TestHumanActorUserIDOnAMoveResetsTheCount() {
	parker, _, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	uid := "firebase-uid-9"
	_, err := s.events.Create(context.Background(), domain.BoardEvent{
		TaskID:      taskID,
		EventType:   domain.BoardEventTaskMoved,
		Payload:     json.RawMessage(`{"from_column":"blocked","to_column":"in_progress"}`),
		ActorUserID: &uid,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Len(s.runner.jobs, 1)
	s.Empty(parker.resources)
}

// The cap is about ARRIVING in need_revision. A task cycling through other
// columns — however many times — is not what it counts, and suppressing those
// would stall work the guard has said nothing about.
func (s *DispatcherSuite) TestOtherColumnsAreNotCapped() {
	parker, _, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	for i := 0; i < 5; i++ {
		s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID: taskID, RepositoryID: repositoryID,
			Column: domain.TaskColumnInProgress, AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"from_column": string(domain.TaskColumnNeedRevision),
			"to_column":   string(domain.TaskColumnInProgress),
		},
	})

	s.Require().NoError(err)
	s.Len(s.runner.jobs, 1, "a move into in_progress is not another revision lap")
	s.Empty(parker.resources)
}

// The ordering the cap turns on: park FIRST, explain afterwards.
//
// Commenting first put the explanation on a card still sitting in
// need_revision, and the comment is itself a dispatch trigger — task.commented
// goes back through Dispatch and resolves the column's agents — so the cap's own
// comment started the developer run it had just refused to start. Held only by
// `blocked`, which suspends dispatch, is the comment inert.
func (s *DispatcherSuite) TestTheParkHappensBeforeTheComment() {
	parker, commenter, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnInProgress, domain.TaskColumnCodeReview, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorSystem)

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Require().Len(parker.resources, 1)
	s.Require().Len(commenter.parked, 1)
	s.True(commenter.parked[0],
		"the card was still dispatchable when the cap commented, and that comment dispatches an agent")
}

// A park that did not take leaves a card nothing is holding. Suppressing its
// dispatch then is the worst of both: no run, no park, no comment, and a card
// that simply went quiet — while every later sweep finds the same count and
// says the same thing again. So a failed park gives up the hold.
func (s *DispatcherSuite) TestAFailedParkDispatchesNormallyAndSaysNothing() {
	parker, commenter, _ := s.installReviewLoopGuard()
	parker.err = errors.New("tasks table is down")
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnInProgress, domain.TaskColumnCodeReview, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorSystem)

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Len(s.runner.jobs, 1, "the card is unparked and undispatched: it would just stop, silently")
	s.Empty(parker.resources)
	s.Empty(commenter.contents, "a comment on an unparked card starts the run the cap refused to start")
}

// The gate is about ARRIVING in need_revision, and only the payload can say
// that. `task.moved` plus "the task is in need_revision" is not the same
// question: Dispatch stamps task.moved on its own housekeeping too, so a
// reconciler revival or a quota resume of a card already sitting there used to
// pass the gate and be counted against a streak it was no part of — the cap
// firing on a dispatch that was not a lap.
func (s *DispatcherSuite) TestDispatchesThatAreNotArrivalsNeverReachTheCap() {
	for name, payload := range map[string]map[string]interface{}{
		"reconciler revival": {
			"column": string(domain.TaskColumnNeedRevision), "reconciled": true,
		},
		"quota resume": {
			"column": string(domain.TaskColumnNeedRevision), "resumed": "quota_reset",
			domain.EventPayloadResumedResource: domain.ResourceClaudeCodeQuota,
		},
		"hand-off inside the column": {
			"from_column": string(domain.TaskColumnNeedRevision),
			"to_column":   string(domain.TaskColumnNeedRevision),
		},
	} {
		s.Run(name, func() {
			s.SetupTest()
			parker, commenter, _ := s.installReviewLoopGuard()
			taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
			for i := 0; i < 3; i++ {
				s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
			}

			err := s.disp.Dispatch(context.Background(), board.DispatchInput{
				RepositoryID: repositoryID,
				Task: domain.BoardTask{
					ID: taskID, RepositoryID: repositoryID, Key: "B-4",
					Column: domain.TaskColumnNeedRevision, AssigneeAgentID: &assignee,
				},
				EventType: domain.BoardEventTaskMoved,
				Payload:   payload,
			})

			s.Require().NoError(err)
			s.Len(s.runner.jobs, 1, "the board's own housekeeping is not another revision lap")
			s.Empty(parker.resources)
			s.Empty(commenter.contents)
		})
	}
}

// An unwired guard is the old behaviour, exactly.
func (s *DispatcherSuite) TestWithoutTheReviewLoopGuardNothingIsCapped() {
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	for i := 0; i < 5; i++ {
		s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	}

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))
	s.Len(s.runner.jobs, 1)
}
