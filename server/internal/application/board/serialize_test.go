package board

import (
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

// Two runs on one task share a checked-out branch and a task workspace. The
// architect dispatched into code_review used to start while the developer's run
// was still in its tail — verification, fix rounds, commit and push — so the
// reviewer read a tree the developer was still writing.
func TestSecondRunOnSameTaskWaitsForTheFirst(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	taskID := uuid.New()
	first, second := jobFor(taskID), jobFor(taskID)

	if !r.beginTask(first) {
		t.Fatal("first run on an idle task must start")
	}
	if r.beginTask(second) {
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

// Serialization is per task: unrelated tasks keep running in parallel across
// the worker pool.
func TestRunsOnDifferentTasksDoNotBlockEachOther(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})

	if !r.beginTask(jobFor(uuid.New())) {
		t.Fatal("first task must start")
	}
	if !r.beginTask(jobFor(uuid.New())) {
		t.Fatal("a run on a different task must not wait")
	}
}

// Parked jobs are released one at a time, oldest first: releasing all of them
// at once would put the whole pile-up back into the exact concurrency this
// exists to prevent.
func TestParkedRunsAreReleasedOneAtATimeInOrder(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	taskID := uuid.New()
	first, second, third := jobFor(taskID), jobFor(taskID), jobFor(taskID)

	r.beginTask(first)
	r.beginTask(second)
	r.beginTask(third)

	r.endTask(taskID)
	released := <-r.queue
	if released.Run.ID != second.Run.ID {
		t.Fatalf("released %s first, want %s", released.Run.ID, second.Run.ID)
	}
	if got := len(r.queue); got != 0 {
		t.Fatalf("third job released before the second finished: queue = %d", got)
	}

	// The released job claims the task, and only its own end releases the next.
	if !r.beginTask(released) {
		t.Fatal("released job must be able to claim the idle task")
	}
	r.endTask(taskID)
	if got := (<-r.queue).Run.ID; got != third.Run.ID {
		t.Fatalf("released %s, want %s", got, third.Run.ID)
	}
}

// endTask on a task with nothing parked must leave the queue alone rather than
// re-enqueue the run that just finished.
func TestEndTaskWithoutParkedRunsIsANoOp(t *testing.T) {
	r := NewRunner(RunnerDeps{Runs: &countingRunStore{}})
	taskID := uuid.New()

	r.beginTask(jobFor(taskID))
	r.endTask(taskID)

	if got := len(r.queue); got != 0 {
		t.Fatalf("queue = %d, want 0", got)
	}
	if !r.beginTask(jobFor(taskID)) {
		t.Fatal("task must be claimable again once its run ended")
	}
}
