package board

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestTriggerMessageDoesNotAskForAMoveIntoTheCurrentColumn(t *testing.T) {
	job := RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnInProgress}}

	msg := buildTriggerMessage(job, taskWF, nil, nil)

	if strings.Contains(msg, "move it to in_progress") {
		t.Fatalf("in_progress task is still told to move itself to in_progress:\n%s", msg)
	}
	if !strings.Contains(msg, "ALREADY in `in_progress`") {
		t.Fatalf("trigger message does not state the task is already in_progress:\n%s", msg)
	}
}

func TestTriggerMessageKeepsTheClaimAndMoveForATodoTask(t *testing.T) {
	job := RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnTodo}}

	msg := buildTriggerMessage(job, taskWF, nil, nil)

	if !strings.Contains(msg, "move it to in_progress") {
		t.Fatalf("a todo task must still be told to claim and move:\n%s", msg)
	}
}

func TestTriggerMessageRulesOutBookkeepingAsAStep(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress,
		domain.TaskColumnNeedRevision, domain.TaskColumnCodeReview,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, taskWF, nil, nil)
		if !strings.Contains(msg, "never a step of its own") {
			t.Errorf("column %s: trigger message allows bookkeeping to become its own step:\n%s", col, msg)
		}
	}
}

func TestTriggerMessageStatesStandingCriteriaForImplementers(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, taskWF, nil, nil)
		for _, want := range []string{
			"Standing acceptance criteria",
			"The project builds.",
			"The whole test suite passes",
			"New or changed behaviour comes with unit tests",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("column %s: trigger message is missing %q:\n%s", col, want, msg)
			}
		}
	}
}

func TestTriggerMessageOmitsStandingCriteriaForAnaliz(t *testing.T) {
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{
		Title: "t", Column: domain.TaskColumnInProgress, TaskType: "analiz",
	}}, analizWF, nil, nil)
	if strings.Contains(msg, "Standing acceptance criteria") {
		t.Errorf("analiz run was handed the implementer's build criteria:\n%s", msg)
	}
}

func TestTriggerMessageListsOpenCriteriaForImplementers(t *testing.T) {
	open := []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "Android button links to the Play Store listing"},
	}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, taskWF, open, nil)
		if !strings.Contains(msg, open[0].Text) {
			t.Errorf("column %s: open criterion is not in the trigger message:\n%s", col, msg)
		}
		if !strings.Contains(msg, open[0].ID.String()) {
			t.Errorf("column %s: criterion id is missing, so set_criterion_completed cannot be called:\n%s", col, msg)
		}
		if !strings.Contains(msg, "set_criterion_completed") {
			t.Errorf("column %s: implementer is not told to tick the criteria:\n%s", col, msg)
		}
	}
}

func TestTriggerMessageDoesNotTellReviewersToTickCriteria(t *testing.T) {
	open := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "criterion"}}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA, domain.TaskColumnPMUAT,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, taskWF, open, nil)
		if !strings.Contains(msg, open[0].Text) {
			t.Errorf("column %s: reviewer should still see the criteria:\n%s", col, msg)
		}
		if strings.Contains(msg, "call set_criterion_completed") {
			t.Errorf("column %s: reviewer is told to tick the developer's claim:\n%s", col, msg)
		}
	}
}

func TestTriggerMessageOmitsTheCriteriaBlockWhenNoneAreOpen(t *testing.T) {
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnInProgress}}, taskWF, nil, nil)
	if strings.Contains(msg, "Open acceptance criteria") {
		t.Fatalf("a task with nothing open still gets a criteria block:\n%s", msg)
	}
}

func TestTriggerMessageCarriesTheTaskType(t *testing.T) {
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{
		Title: "t", Column: domain.TaskColumnTodo, TaskType: "analiz",
	}}, analizWF, nil, nil)

	if !strings.Contains(msg, `"task_type":"analiz"`) {
		t.Fatalf("task snapshot does not carry the task type:\n%s", msg)
	}
}

func TestAnalizInstructionDoesNotAskForCodeOrACodeReviewHandoff(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		instruction := columnInstruction(analizWF, domain.BoardTask{Column: col, TaskType: "analiz"})
		if strings.Contains(instruction, "the system moves the task to code_review") {
			t.Errorf("column %s: an analiz run is promised a code_review hand-off it never gets:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "no automatic hand-off to code_review") {
			t.Errorf("column %s: an analiz run is not told the code_review hand-off does not apply to it:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "add_task_document") {
			t.Errorf("column %s: an analiz run is not told its deliverable is a document:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "analiz_review") {
			t.Errorf("column %s: an analiz run is not told where the human gate is:\n%s", col, instruction)
		}
		if !strings.Contains(instruction, "Never write, edit, move or delete a file") {
			t.Errorf("column %s: an analiz run is not told to keep its hands off the repo:\n%s", col, instruction)
		}
	}
}

func TestAnalizInstructionSplitsTheHumanGateFromTheApproval(t *testing.T) {
	waiting := columnInstruction(analizWF, domain.BoardTask{Column: domain.TaskColumnAnalizReview, TaskType: "analiz"})
	if !strings.Contains(waiting, "Take no action") {
		t.Errorf("analiz_review is a human gate; the agent must stand down:\n%s", waiting)
	}
	approved := columnInstruction(analizWF, domain.BoardTask{Column: domain.TaskColumnDone, TaskType: "analiz"})
	if !strings.Contains(approved, "released") || !strings.Contains(approved, "list_team") {
		t.Errorf("an approved analiz must be decomposed and released:\n%s", approved)
	}
}

func TestImplementationTasksKeepTheColumnInstruction(t *testing.T) {
	for _, typ := range []domain.TaskType{"task", "bug", ""} {
		instruction := columnInstruction(taskWF, domain.BoardTask{Column: domain.TaskColumnInProgress, TaskType: typ})
		if !strings.Contains(instruction, "code_review") {
			t.Errorf("task type %q lost the implementer hand-off:\n%s", typ, instruction)
		}
	}
}

func TestImplementerInstructionNamesTheCriteriaGate(t *testing.T) {
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		instruction := columnInstruction(taskWF, domain.BoardTask{Column: col})
		if !strings.Contains(instruction, "set_criterion_completed") {
			t.Errorf("column %s: instruction does not mention ticking criteria:\n%s", col, instruction)
		}
	}
}
