package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// questionTaskManager is a minimal, in-memory QuestionManager for the record/
// list tool tests, mirroring reviewTaskManager's relationship to
// AnnotationManager.
type questionTaskManager struct {
	*documentTaskManager
	items []domain.TaskQuestion
	err   error
}

func (q *questionTaskManager) RecordQuestions(_ context.Context, _, taskID uuid.UUID, add []domain.NewQuestionInput, update []domain.UpdateQuestionInput, withdraw []string) ([]domain.TaskQuestion, error) {
	if q.err != nil {
		return nil, q.err
	}
	if q.documentTaskManager.task.TaskType != domain.TaskTypeAnaliz {
		return nil, errors.New("record_open_questions is for analiz tasks only")
	}
	for _, in := range add {
		if err := in.Validate(); err != nil {
			return nil, err
		}
		q.items = append(q.items, domain.TaskQuestion{
			ID: uuid.New(), TaskID: taskID, Key: "Q" + itoa(len(q.items)+1),
			Prompt: in.Prompt, Kind: in.Kind, Blocking: in.Blocking, RecommendedAnswer: in.RecommendedAnswer,
			Status: domain.QuestionStatusOpen,
		})
	}
	for _, upd := range update {
		for i, existing := range q.items {
			if existing.Key != upd.Key {
				continue
			}
			next, err := domain.ApplyQuestionUpdate(existing, upd)
			if err != nil {
				return nil, err
			}
			q.items[i] = next
		}
	}
	for _, key := range withdraw {
		for i, existing := range q.items {
			if existing.Key == key {
				q.items[i].Status = domain.QuestionStatusWithdrawn
			}
		}
	}
	return q.items, nil
}

func (q *questionTaskManager) ListQuestions(context.Context, uuid.UUID, uuid.UUID) ([]domain.TaskQuestion, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.items, nil
}

func newQuestionKit(t *testing.T, taskType domain.TaskType) (*ToolKit, *questionTaskManager, uuid.UUID) {
	t.Helper()
	kit, tasks, taskID := newDocumentKit(taskType)
	qtm := &questionTaskManager{documentTaskManager: tasks}
	kit.Tasks = qtm
	return kit, qtm, taskID
}

func TestRecordOpenQuestionsAddsWithGeneratedKeys(t *testing.T) {
	kit, mgr, taskID := newQuestionKit(t, "analiz")
	tool := newRecordQuestionsTool(kit)

	res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","questions":[`+
		`{"prompt":"Which queue?","kind":"technical","blocking":true},`+
		`{"prompt":"Keep the old export format?","kind":"product","blocking":false,"recommended_answer":"Keep it."}]}`)

	require.False(t, res.IsError, res.Content)
	require.Len(t, mgr.items, 2)
	assert.Equal(t, "Q1", mgr.items[0].Key)
	assert.Equal(t, "Q2", mgr.items[1].Key)
	out := decode(t, res.Content)
	assert.EqualValues(t, 2, out["count"])
}

func TestRecordOpenQuestionsRefusesNonBlockingWithoutRecommendedAnswer(t *testing.T) {
	kit, mgr, taskID := newQuestionKit(t, "analiz")
	tool := newRecordQuestionsTool(kit)

	res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","questions":[`+
		`{"prompt":"Keep the old export format?","kind":"product","blocking":false}]}`)

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "recommended_answer")
	assert.Empty(t, mgr.items)
}

func TestRecordOpenQuestionsRefusesEditingAnAnsweredPrompt(t *testing.T) {
	kit, mgr, taskID := newQuestionKit(t, "analiz")
	now := time.Now().UTC()
	answered, err := domain.ApplyQuestionAnswer(domain.TaskQuestion{
		Key: "Q1", Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true,
	}, "Use SQS.", now)
	require.NoError(t, err)
	mgr.items = []domain.TaskQuestion{answered}
	tool := newRecordQuestionsTool(kit)

	res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","update":[`+
		`{"key":"Q1","prompt":"Which queue should the worker use?"}]}`)

	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "already answered")
	assert.Equal(t, "Which queue?", mgr.items[0].Prompt, "the stored prompt is unchanged")
}

func TestRecordOpenQuestionsWithdraw(t *testing.T) {
	kit, mgr, taskID := newQuestionKit(t, "analiz")
	mgr.items = []domain.TaskQuestion{{Key: "Q1", Prompt: "q", Kind: domain.QuestionKindTechnical, RecommendedAnswer: "a", Status: domain.QuestionStatusOpen}}
	tool := newRecordQuestionsTool(kit)

	res := tool.Execute(context.Background(), `{"task_id":"`+taskID.String()+`","withdraw":["Q1"]}`)

	require.False(t, res.IsError, res.Content)
	assert.Equal(t, domain.QuestionStatusWithdrawn, mgr.items[0].Status)
}

func TestListOpenQuestionsReturnsAnswers(t *testing.T) {
	kit, mgr, taskID := newQuestionKit(t, "analiz")
	mgr.items = []domain.TaskQuestion{
		{Key: "Q1", Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true, Status: domain.QuestionStatusAnswered, Answer: "Use SQS."},
	}
	res := newListOpenQuestionsTool(kit).Execute(context.Background(), `{"task_id":"`+taskID.String()+`"}`)

	require.False(t, res.IsError, res.Content)
	out := decode(t, res.Content)
	item := out["questions"].([]any)[0].(map[string]any)
	assert.Equal(t, "Use SQS.", item["answer"])
	assert.Equal(t, "answered", item["status"])
}

func TestQuestionToolsSayWhenTheBuildCannotServeThem(t *testing.T) {
	kit, _, taskID := newDocumentKit("analiz")
	res := newListOpenQuestionsTool(kit).Execute(context.Background(), `{"task_id":"`+taskID.String()+`"}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "cannot read open questions")
}
