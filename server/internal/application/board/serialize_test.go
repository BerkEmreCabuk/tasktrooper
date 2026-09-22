package board

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func jobFor(taskID uuid.UUID) RunJob {
	return RunJob{
		Run:  domain.TaskAgentRun{ID: uuid.New(), TaskID: taskID},
		Task: domain.BoardTask{ID: taskID},
	}
}

func TestSecondRunOnSameTaskWaitsForTheFirst(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	taskID := uuid.New()
	first, second := jobFor(taskID), jobFor(taskID)

	if !r.beginTask(context.Background(), first) {
		t.Fatal("first run on an idle task must start")
	}
	if r.beginTask(context.Background(), second) {
		t.Fatal("second run started while the first still holds the task")
	}
	if got := len(r.queue); got != 0 {
		t.Fatalf("parked job requeued too early: queue = %d", got)
	}

	r.endTask(taskID)

	select {
	case released := <-r.queue:
		if released.Run.ID != second.Run.ID {
			t.Fatalf("released job = %s, want the parked one %s", released.Run.ID, second.Run.ID)
		}
	default:
		t.Fatal("parked job was not requeued when the task went idle")
	}
}

func TestRunsOnDifferentTasksDoNotBlockEachOther(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})

	if !r.beginTask(context.Background(), jobFor(uuid.New())) {
		t.Fatal("first task must start")
	}
	if !r.beginTask(context.Background(), jobFor(uuid.New())) {
		t.Fatal("a run on a different task must not wait")
	}
}

func TestParkedRunsAreReleasedOneAtATimeInOrder(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	taskID := uuid.New()
	first, second, third := jobFor(taskID), jobFor(taskID), jobFor(taskID)

	r.beginTask(context.Background(), first)
	r.beginTask(context.Background(), second)
	r.beginTask(context.Background(), third)

	r.endTask(taskID)
	released := <-r.queue
	if released.Run.ID != second.Run.ID {
		t.Fatalf("released %s first, want %s", released.Run.ID, second.Run.ID)
	}
	if got := len(r.queue); got != 0 {
		t.Fatalf("third job released before the second finished: queue = %d", got)
	}

	if !r.beginTask(context.Background(), released) {
		t.Fatal("released job must be able to claim the idle task")
	}
	r.endTask(taskID)
	if got := (<-r.queue).Run.ID; got != third.Run.ID {
		t.Fatalf("released %s, want %s", got, third.Run.ID)
	}
}

func TestEndTaskWithoutParkedRunsIsANoOp(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	taskID := uuid.New()

	r.beginTask(context.Background(), jobFor(taskID))
	r.endTask(taskID)

	if got := len(r.queue); got != 0 {
		t.Fatalf("queue = %d, want 0", got)
	}
	if !r.beginTask(context.Background(), jobFor(taskID)) {
		t.Fatal("task must be claimable again once its run ended")
	}
}
