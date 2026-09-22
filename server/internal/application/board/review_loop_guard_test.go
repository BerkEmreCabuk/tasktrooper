package board_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

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

func (s *DispatcherSuite) TestHumanEventResetsTheReviewLoopCount() {
	parker, commenter, _ := s.installReviewLoopGuard()
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	s.seedHumanComment(taskID)

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))

	s.Len(s.runs.runs, 1, "the rounds before a human intervened are ones they have already seen")
	s.Len(s.runner.jobs, 1)
	s.Empty(parker.resources)
	s.Empty(commenter.contents)
}

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

func (s *DispatcherSuite) TestWithoutTheReviewLoopGuardNothingIsCapped() {
	taskID, repositoryID, assignee := uuid.New(), uuid.New(), uuid.New()
	for i := 0; i < 5; i++ {
		s.seedMove(taskID, domain.TaskColumnCodeReview, domain.TaskColumnNeedRevision, domain.EventActorAgent)
	}

	s.Require().NoError(s.dispatchIntoNeedRevision(repositoryID, taskID, assignee))
	s.Len(s.runner.jobs, 1)
}
