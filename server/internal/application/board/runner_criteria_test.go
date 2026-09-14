package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The failure this file exists for: by the time QA is dispatched the developer
// has ticked every acceptance criterion, so the "open criteria" list the prompt
// carried was EMPTY and QA saw no criterion id at all. review_criterion takes a
// uuid; with none in reach the run passed the criterion text, was told "invalid
// criterion_id", and moved the task to need_revision to make the ids appear —
// bouncing finished work back to a developer with nothing to fix, once per
// review cycle.
func TestCriteriaForRunListsEveryCriterionForReviewColumns(t *testing.T) {
	ticked := []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "stage login accepts a valid password", Completed: true},
		{ID: uuid.New(), Text: "a wrong password shows the error banner", Completed: true},
	}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
		domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT,
	} {
		r := NewRunner(RunnerDeps{})
		r.SetTaskUpdater(&criteriaUpdater{criteria: ticked})
		job := RunJob{Task: domain.BoardTask{ID: uuid.New(), Title: "t", Column: col}, RepositoryID: uuid.New()}

		got := r.criteriaForRun(context.Background(), job)
		require.Len(t, got, len(ticked), "column %s: a reviewing run must see every criterion, ticked or not", col)

		msg := buildTriggerMessage(job, got)
		for _, c := range ticked {
			if !strings.Contains(msg, c.ID.String()) {
				t.Errorf("column %s: criterion id %s is missing, so review_criterion cannot be called:\n%s", col, c.ID, msg)
			}
			if !strings.Contains(msg, c.Text) {
				t.Errorf("column %s: criterion text is missing:\n%s", col, msg)
			}
		}
	}
}

// The QA and PM headers have to say three things the transcripts show were all
// missing, the last one loudest: bouncing the task to need_revision is not how
// you find these ids.
func TestTriggerMessageTellsReviewersToRecordAVerdictOnEachID(t *testing.T) {
	criteria := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "criterion", Completed: true}}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA, domain.TaskColumnInQA, domain.TaskColumnPMUAT,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, criteria)
		for _, want := range []string{
			"review_criterion",
			"The forward move is refused while any id lacks your verdict on repositories that require criteria — record them all regardless.",
			"Never move the task to need_revision just to look for these ids",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("column %s: trigger message is missing %q:\n%s", col, want, msg)
			}
		}
		if strings.Contains(msg, "Open acceptance criteria") {
			t.Errorf("column %s: reviewer got the implementer's open-only checklist:\n%s", col, msg)
		}
	}
}

func TestTriggerMessageNamesTheCriteriaAsTheDiffsTargetInCodeReview(t *testing.T) {
	criteria := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "criterion", Completed: true}}
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnCodeReview}}, criteria)

	require.Contains(t, msg, "Acceptance criteria the diff must satisfy (ids for reference)")
	require.NotContains(t, msg, "call set_criterion_completed", "the reviewer does not tick the developer's boxes")
}

// A reviewer that cannot see an existing verdict either repeats work another
// role recorded or reads the developer's checkmark as an approval.
func TestTriggerMessageShowsWhoHasSaidWhatAboutEachCriterion(t *testing.T) {
	c := domain.AcceptanceCriterion{
		ID: uuid.New(), Text: "the banner appears", Completed: true,
		Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true}},
	}
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnPMUAT}},
		[]domain.AcceptanceCriterion{c})

	want := "- [" + c.ID.String() + "] the banner appears — implementer: ticked; qa: approved; pm: —"
	if !strings.Contains(msg, want) {
		t.Fatalf("criterion state line is missing %q:\n%s", want, msg)
	}
}

func TestTriggerMessageShowsARejectionWithItsNote(t *testing.T) {
	c := domain.AcceptanceCriterion{
		ID: uuid.New(), Text: "the banner appears", Completed: true,
		Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: false, Note: "banner never rendered"}},
	}
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnPMUAT}},
		[]domain.AcceptanceCriterion{c})

	require.Contains(t, msg, "qa: rejected (banner never rendered)")
}

// The implementer's half is unchanged: it is told what is LEFT to do, not what
// it has already ticked.
func TestCriteriaForRunListsOnlyOpenCriteriaForImplementers(t *testing.T) {
	done := domain.AcceptanceCriterion{ID: uuid.New(), Text: "already implemented", Completed: true}
	open := domain.AcceptanceCriterion{ID: uuid.New(), Text: "still to do", Completed: false}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		r := NewRunner(RunnerDeps{})
		r.SetTaskUpdater(&criteriaUpdater{criteria: []domain.AcceptanceCriterion{done, open}})
		job := RunJob{Task: domain.BoardTask{ID: uuid.New(), Title: "t", Column: col}, RepositoryID: uuid.New()}

		got := r.criteriaForRun(context.Background(), job)
		require.Len(t, got, 1, "column %s: an implementer sees only what is still open", col)
		require.Equal(t, open.ID, got[0].ID)

		msg := buildTriggerMessage(job, got)
		if !strings.Contains(msg, "Open acceptance criteria") {
			t.Errorf("column %s: implementer lost the open-criteria checklist:\n%s", col, msg)
		}
		if strings.Contains(msg, done.ID.String()) {
			t.Errorf("column %s: a ticked criterion is back in the implementer's list:\n%s", col, msg)
		}
		if !strings.Contains(msg, "call set_criterion_completed with its id") {
			t.Errorf("column %s: implementer is not told to tick the criteria:\n%s", col, msg)
		}
	}
}

// An analysis ticks its own criteria as its documents answer them; nothing about
// that changes because a review column now lists everything.
//
// The column has to be one listsEveryCriterion actually lists — code_review —
// or the assertion proves nothing: analiz_review is rejected by the column
// switch before the TaskTypeAnaliz guard is ever consulted, so an analiz task
// sitting there would keep the open-only list even if the guard were deleted.
func TestCriteriaForRunKeepsAnalizBehaviourUnchanged(t *testing.T) {
	done := domain.AcceptanceCriterion{ID: uuid.New(), Text: "answered", Completed: true}
	open := domain.AcceptanceCriterion{ID: uuid.New(), Text: "unanswered", Completed: false}
	r := NewRunner(RunnerDeps{})
	r.SetTaskUpdater(&criteriaUpdater{criteria: []domain.AcceptanceCriterion{done, open}})
	job := RunJob{Task: domain.BoardTask{
		ID: uuid.New(), Title: "t", Column: domain.TaskColumnCodeReview, TaskType: domain.TaskTypeAnaliz,
	}, RepositoryID: uuid.New()}

	require.False(t, listsEveryCriterion(job.Task),
		"code_review is listed, so only the analiz guard can hold this false")

	got := r.criteriaForRun(context.Background(), job)
	require.Len(t, got, 1)
	require.Equal(t, open.ID, got[0].ID)

	msg := buildTriggerMessage(job, got)
	require.Contains(t, msg, "Open acceptance criteria", "an analiz run keeps the implementer's open-only checklist")
	require.NotContains(t, msg, done.ID.String())
	require.NotContains(t, msg, "Acceptance criteria the diff must satisfy")
}

// get_deploy_target was called with "agent-server" — the repository NAME, the
// only repository identifier a run could see — and answered "invalid
// repository_id", with nowhere to look the uuid up.
func TestTriggerMessageSnapshotCarriesTheRepositoryID(t *testing.T) {
	repoID := uuid.New()
	msg := buildTriggerMessage(RunJob{
		Task:         domain.BoardTask{Title: "t", Column: domain.TaskColumnInProgress},
		RepositoryID: repoID,
	}, nil)

	if !strings.Contains(msg, `"repository_id":"`+repoID.String()+`"`) {
		t.Fatalf("task snapshot does not carry the repository id:\n%s", msg)
	}
	if !strings.Contains(msg, "want the repository_id UUID from the snapshot below — never the repository name") {
		t.Fatalf("trigger message does not tell the run which repository identifier to pass:\n%s", msg)
	}
}
