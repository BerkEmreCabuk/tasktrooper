package board

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeQuestionsTaskUpdater struct {
	fakeTaskUpdater
	byTask map[uuid.UUID][]domain.TaskQuestion
	refs   []domain.AnalysisReference
}

func (f *fakeQuestionsTaskUpdater) ListQuestionsByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskQuestion, error) {
	return f.byTask[taskID], nil
}

func (f *fakeQuestionsTaskUpdater) AnalysisReferences(context.Context, uuid.UUID) ([]domain.AnalysisReference, error) {
	return f.refs, nil
}

func analizJob(taskID uuid.UUID) RunJob {
	return runJobFor(domain.BoardTask{ID: taskID, Key: "A-7", Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz}, uuid.New())
}

func TestBlockOnPendingQuestionsParksOnlyForAPendingBlockingQuestion(t *testing.T) {
	taskID := uuid.New()
	blocker := &blockRecorder{}
	updater := &fakeQuestionsTaskUpdater{byTask: map[uuid.UUID][]domain.TaskQuestion{
		taskID: {
			{Key: "Q1", Prompt: "Which queue should this use?", Blocking: true, Status: domain.QuestionStatusOpen},
			{Key: "Q2", Prompt: "Keep the old export format?", Blocking: false, Status: domain.QuestionStatusOpen, RecommendedAnswer: "Keep it."},
		},
	}}
	r := &Runner{taskUpdater: updater, blocker: blocker, parks: NewParkJournal(nil, nil)}

	blocked := r.blockOnPendingQuestions(context.Background(), analizJob(taskID))

	assert.True(t, blocked)
	resource, detail := blocker.parked()
	assert.Equal(t, domain.ResourceAnalysisQuestions, resource)
	assert.Contains(t, detail, "Q1: Which queue should this use?")
	assert.NotContains(t, detail, "Q2", "only the pending BLOCKING question names itself in the detail")
}

func TestBlockOnPendingQuestionsNoOpWithoutAPendingBlockingQuestion(t *testing.T) {
	taskID := uuid.New()
	blocker := &blockRecorder{}
	updater := &fakeQuestionsTaskUpdater{byTask: map[uuid.UUID][]domain.TaskQuestion{
		taskID: {
			{Key: "Q1", Prompt: "Keep the old export format?", Blocking: false, Status: domain.QuestionStatusOpen, RecommendedAnswer: "Keep it."},
			{Key: "Q2", Prompt: "Already answered", Blocking: true, Status: domain.QuestionStatusAnswered, Answer: "yes"},
		},
	}}
	r := &Runner{taskUpdater: updater, blocker: blocker, parks: NewParkJournal(nil, nil)}

	assert.False(t, r.blockOnPendingQuestions(context.Background(), analizJob(taskID)))
	resource, _ := blocker.parked()
	assert.Empty(t, resource, "nothing pending-blocking means no block call at all")
}

func TestBlockOnPendingQuestionsIsNoOpForNonAnalizTasks(t *testing.T) {
	taskID := uuid.New()
	blocker := &blockRecorder{}
	updater := &fakeQuestionsTaskUpdater{byTask: map[uuid.UUID][]domain.TaskQuestion{
		taskID: {{Key: "Q1", Prompt: "x", Blocking: true, Status: domain.QuestionStatusOpen}},
	}}
	r := &Runner{taskUpdater: updater, blocker: blocker, parks: NewParkJournal(nil, nil)}
	job := runJobFor(domain.BoardTask{ID: taskID, Column: domain.TaskColumnInProgress, TaskType: "task"}, uuid.New())

	assert.False(t, r.blockOnPendingQuestions(context.Background(), job))
	resource, _ := blocker.parked()
	assert.Empty(t, resource)
}

func TestOpenQuestionsContextCoversTheTaskItselfAndDerivedTasks(t *testing.T) {
	selfID := uuid.New()
	derivedAnalizID := uuid.New()
	updater := &fakeQuestionsTaskUpdater{
		byTask: map[uuid.UUID][]domain.TaskQuestion{
			selfID: {
				{Key: "Q1", Prompt: "Which queue?", Kind: "technical", Blocking: true, Status: domain.QuestionStatusOpen},
			},
			derivedAnalizID: {
				{Key: "Q1", Prompt: "Keep the old export format?", Kind: "product", Status: domain.QuestionStatusAnswered, Answer: "Yes, keep it.", RecommendedAnswer: "Keep it."},
				{Key: "Q2", Prompt: "Withdrawn one", Status: domain.QuestionStatusWithdrawn},
			},
		},
		refs: []domain.AnalysisReference{{TaskID: derivedAnalizID, Key: "A-12", Title: "CSV export"}},
	}
	r := &Runner{taskUpdater: updater}

	msg := r.openQuestionsContext(context.Background(), analizJob(selfID))

	require.NotEmpty(t, msg)
	assert.Contains(t, msg, "Which queue?", "the task's own questions are included")
	assert.Contains(t, msg, "Unanswered (blocking)")
	assert.Contains(t, msg, "A-12 (CSV export)", "the derived-from analiz task is named")
	assert.Contains(t, msg, "Keep the old export format?")
	assert.Contains(t, msg, "Answer (human): Yes, keep it.")
	assert.NotContains(t, msg, "Withdrawn one", "withdrawn questions are left out of run context")
}

func TestOpenQuestionsContextEmptyWithNoQuestionsAnywhere(t *testing.T) {
	updater := &fakeQuestionsTaskUpdater{}
	r := &Runner{taskUpdater: updater}
	assert.Empty(t, r.openQuestionsContext(context.Background(), analizJob(uuid.New())))
}

// TestGoldenOpenQuestionsContextBlock pins the exact catalog rendering for
// one of each question state: open+blocking, open+non-blocking (recommended
// answer stands), answered, and withdrawn (left out entirely).
func TestGoldenOpenQuestionsContextBlock(t *testing.T) {
	items := []domain.TaskQuestion{
		{Key: "Q1", Kind: domain.QuestionKindTechnical, Blocking: true, Prompt: "Which queue should the worker use?", Status: domain.QuestionStatusOpen},
		{Key: "Q2", Kind: domain.QuestionKindProduct, Prompt: "Keep the old CSV export format?", RecommendedAnswer: "Keep it unchanged.", Status: domain.QuestionStatusOpen},
		{Key: "Q3", Kind: domain.QuestionKindProduct, Prompt: "Should the report link to the raw diff?", Status: domain.QuestionStatusAnswered, Answer: "No, link to the PR instead."},
		{Key: "Q4", Kind: domain.QuestionKindTechnical, Blocking: true, Prompt: "withdrawn one", Status: domain.QuestionStatusWithdrawn},
	}
	assertGolden(t, "open_questions_context", renderQuestionsBlock(items, "A-12 (CSV export)", "A-12"))
}
