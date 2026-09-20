package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// These five strings are byte-for-byte copies of what
// board.taskTypeInstruction(domain.BoardTask{TaskType: "analiz", ...})
// returned at each column, and of board.analizProducesDocuments, captured
// immediately before both were deleted in favour of reading
// domain.WorkflowStage.Instructions off the workflow. This file is the proof
// WP-B2b's task required before that deletion: that migration 143 (via
// workflowtest.Default(), which the migration's own parity test asserts
// against byte-for-byte) writes the identical prompt text the old
// column/type-literal switch produced. Nothing here should ever need to
// change again — a real wording change belongs in migration 143 and
// workflowtest/stages.go, and this test would then need updating deliberately
// alongside it, not as a side effect of something else.
const goldenAnalizProducesDocuments = "Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, " +
	"grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — " +
	"a document attached by a run that explored nothing is rejected and the run is failed. " +
	"If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. " +
	"Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, " +
	"and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer's job on the wrong task. " +
	"Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: " +
	"when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — " +
	"the human approves there, and no implementation task is created before they do."

var goldenAnalizStageInstructions = map[domain.TaskColumn]string{
	domain.TaskColumnTodo: "This is an ANALIZ task (task_type=analiz) in `todo` — an ANALYSIS, not an implementation. If it is not relevant to your role, take no action. " +
		"If it is: claim it and move it to in_progress as the opening action of the step that does the analysis (never a step of its own), " +
		"then investigate in this same run — clone/pull every repository the task names, read the relevant code, and decide WHAT is needed and WHERE. " +
		goldenAnalizProducesDocuments,
	domain.TaskColumnInProgress: "This is an ANALIZ task (task_type=analiz) ALREADY claimed and ALREADY in `in_progress` — an ANALYSIS, not an implementation, " +
		"and the move you might be tempted to plan first has happened. Continue the investigation from where it stands and finish it in this run. " +
		goldenAnalizProducesDocuments,
	domain.TaskColumnNeedRevision: "This is an ANALIZ task (task_type=analiz) in `need_revision`: the human rejected the analysis. Their comment is in the task comments in your context. " +
		"Revise the spec/plan at the ROOT of the concern — re-read the code where you are unsure — and attach the corrected documents. " +
		"Create no implementation task from a rejected analysis. " + goldenAnalizProducesDocuments,
	domain.TaskColumnAnalizReview: "This is an ANALIZ task (task_type=analiz) in `analiz_review`: it is waiting on a HUMAN to approve or reject the spec/plan. " +
		"Nothing is yours to do here — do not move it, do not rewrite the documents, and do not create implementation tasks. Take no action.",
	domain.TaskColumnDone: "This is an ANALIZ task (task_type=analiz) the human moved to `done` — that move IS the approval of your spec and plan. " +
		"Now decompose it: one implementation task per repository and per layer, each with its own plan slice, testable acceptance criteria and an assignee " +
		"(call list_team for the roster; order them by dependency — backend API before the frontend/mobile that consumes it). " +
		"Write no code yourself. List the created tasks in a comment and move this analiz task to `released` as the last action of the step that created them.",
}

// TestGoldenAnalizStageInstructionsMatchOldTaskTypeInstruction pins
// workflowtest.Default()'s analiz WorkflowStage.Instructions — and therefore
// migration 143's seeded rows, which the migration's own parity test asserts
// equal workflowtest.Default() column for column — against the exact text the
// deleted board.taskTypeInstruction/analizProducesDocuments produced for the
// same five columns. A change to either side that is not a deliberate,
// matching change to the other fails here first.
func TestGoldenAnalizStageInstructionsMatchOldTaskTypeInstruction(t *testing.T) {
	wf := workflowtest.Default().Workflows[domain.TaskType("analiz")]

	for col, want := range goldenAnalizStageInstructions {
		stage, ok := wf.Stage(col)
		if !ok {
			t.Fatalf("analiz workflow has no stage for column %s", col)
		}
		if stage.Instructions != want {
			t.Fatalf("analiz stage instructions for %s do not match the deleted taskTypeInstruction output.\ngot:  %q\nwant: %q",
				col, stage.Instructions, want)
		}
	}

	// Every other column of the analiz workflow carried no type instruction —
	// taskTypeInstruction's default case returned "" for them, so
	// stage.Instructions must be "" too.
	for _, stage := range wf.Stages {
		if _, isGolden := goldenAnalizStageInstructions[stage.Column]; isGolden {
			continue
		}
		if stage.Instructions != "" {
			t.Fatalf("analiz column %s carries instructions the old taskTypeInstruction never produced: %q", stage.Column, stage.Instructions)
		}
	}
}

// TestGoldenNonAnalizStageInstructionsAreEmpty pins the other half of
// taskTypeInstruction's contract: it returned "" for every column of every
// non-analiz type, so columnInstruction fell through to its own per-column
// switch. workflowtest.Default() must carry the same emptiness for task, bug
// and technical, or columnInstruction would start preferring a stage
// instruction those types never had before.
func TestGoldenNonAnalizStageInstructionsAreEmpty(t *testing.T) {
	for _, taskType := range []domain.TaskType{
		domain.TaskType("task"), domain.TaskType("bug"), domain.TaskType("technical"),
	} {
		wf := workflowtest.Default().Workflows[taskType]
		for _, stage := range wf.Stages {
			if stage.Instructions != "" {
				t.Fatalf("%s column %s carries stage instructions, but taskTypeInstruction never produced any for a non-analiz type: %q",
					taskType, stage.Column, stage.Instructions)
			}
		}
	}
}
