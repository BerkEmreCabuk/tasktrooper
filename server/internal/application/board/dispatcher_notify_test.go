package board_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordingNotifier struct {
	moved   []domain.BoardTask
	resumed []resumeCall
}

type resumeCall struct {
	task     domain.BoardTask
	resource string
}

func (n *recordingNotifier) TaskMoved(_ context.Context, task domain.BoardTask) {
	n.moved = append(n.moved, task)
}

func (n *recordingNotifier) TaskResumed(_ context.Context, task domain.BoardTask, resource string) {
	n.resumed = append(n.resumed, resumeCall{task: task, resource: resource})
}

func (n *recordingNotifier) total() int { return len(n.moved) + len(n.resumed) }

func notifyingDispatcher(n board.TaskNotifier) *board.Dispatcher {
	d := board.NewDispatcher(&fakeBoardConfigStore{}, &fakeEventStore{}, &fakeRunStore{}, &fakeRunner{}, true)
	d.SetNotifier(n)
	return d
}

func resumedTask() domain.BoardTask {
	id := uuid.New()
	return domain.BoardTask{
		ID:           id,
		RepositoryID: uuid.New(),
		Key:          "T-42",
		Title:        "parked task",
		Column:       domain.TaskColumnInProgress,
	}
}
func TestSweeperResumesNotifyAsResumed(t *testing.T) {
	// The payloads the four sweepers actually build, key for key.
	for name, payload := range map[string]map[string]interface{}{
		"quota":      {"resumed": "quota_reset", "resource": domain.ResourceClaudeCodeQuota},
		"device":     {"resumed": "device_free", "resource": domain.ResourceMobileDevice},
		"deploy":     {"resumed": "deploy_settled", "resource": domain.ResourceDeployWatch, domain.EventPayloadResumedResource: domain.ResourceDeployWatch},
		"work_order": {"resumed": "work_order_clear", "resource": domain.ResourceWorkOrder, domain.EventPayloadResumedResource: domain.ResourceWorkOrder},
	} {
		t.Run(name, func(t *testing.T) {
			notifier := &recordingNotifier{}
			task := resumedTask()
			payload[domain.EventPayloadActor] = domain.EventActorSystem

			require.NoError(t, notifyingDispatcher(notifier).Dispatch(context.Background(), board.DispatchInput{
				RepositoryID: task.RepositoryID,
				Task:         task,
				EventType:    domain.BoardEventTaskMoved,
				Payload:      payload,
			}))

			assert.Equal(t, 1, notifier.total(), "exactly one notification per resume")
			require.Len(t, notifier.resumed, 1)
			assert.Equal(t, payload["resource"], notifier.resumed[0].resource)
			assert.Equal(t, task.ID, notifier.resumed[0].task.ID)
		})
	}
}

func TestResumeIntoANotifiedColumnStillSendsOnlyTheResume(t *testing.T) {
	notifier := &recordingNotifier{}
	task := resumedTask()
	task.Column = domain.TaskColumnNeedRevision

	require.NoError(t, notifyingDispatcher(notifier).Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":                 "quota_reset",
			"resource":                domain.ResourceClaudeCodeQuota,
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonQuotaRenewed,
		},
	}))

	assert.Empty(t, notifier.moved, "the generic column push must not go out alongside the resume")
	assert.Len(t, notifier.resumed, 1)
}

func TestOrdinaryMoveStillNotifiesAsMoved(t *testing.T) {
	notifier := &recordingNotifier{}
	task := resumedTask()
	task.Column = domain.TaskColumnDone

	require.NoError(t, notifyingDispatcher(notifier).Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload:      map[string]interface{}{"from_column": "in_qa", "to_column": "done"},
	}))

	assert.Len(t, notifier.moved, 1)
	assert.Empty(t, notifier.resumed)
}

func TestNonParkResumesAreNotReportedAsResumed(t *testing.T) {
	for name, payload := range map[string]map[string]interface{}{
		// resume.go — a human answered the clarification. They are holding the
		// phone they just typed it on.
		"question answered": {"resumed": "question_answered", "question": "which env?", "answer": "stage"},
		// runtime.go — the USD budget period rolled over. No resource involved.
		"billing period renewed": {"resumed": "quota_renewed"},
		// release_rollback.go — carries the resumed-resource marker but is a
		// rollback dispatch, not a park release.
		"release rollback": {"release_rollback": true, domain.EventPayloadResumedResource: domain.ResourceDeployWatch},
	} {
		t.Run(name, func(t *testing.T) {
			notifier := &recordingNotifier{}
			task := resumedTask()
			payload[domain.EventPayloadActor] = domain.EventActorSystem

			require.NoError(t, notifyingDispatcher(notifier).Dispatch(context.Background(), board.DispatchInput{
				RepositoryID: task.RepositoryID,
				Task:         task,
				EventType:    domain.BoardEventTaskMoved,
				Payload:      payload,
			}))

			assert.Empty(t, notifier.resumed)
			assert.Len(t, notifier.moved, 1, "it is still an ordinary move")
		})
	}
}

func TestResumeDispatchesWithoutANotifier(t *testing.T) {
	d := board.NewDispatcher(&fakeBoardConfigStore{}, &fakeEventStore{}, &fakeRunStore{}, &fakeRunner{}, true)
	task := resumedTask()

	assert.NoError(t, d.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload:      map[string]interface{}{"resumed": "quota_reset", "resource": domain.ResourceClaudeCodeQuota},
	}))
}
