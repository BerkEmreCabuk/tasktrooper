package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestNewQuestionInputValidate(t *testing.T) {
	in := domain.NewQuestionInput{Prompt: "  Which queue?  ", Kind: domain.QuestionKindTechnical, Blocking: true}
	require.NoError(t, in.Validate())
	assert.Equal(t, "Which queue?", in.Prompt, "trimmed")

	nonBlockingNoDefault := domain.NewQuestionInput{Prompt: "x", Kind: domain.QuestionKindProduct, Blocking: false}
	err := nonBlockingNoDefault.Validate()
	assert.ErrorIs(t, err, domain.ErrQuestionInvalid)
	assert.Contains(t, err.Error(), "recommended_answer")

	nonBlockingWithDefault := domain.NewQuestionInput{Prompt: "x", Kind: domain.QuestionKindProduct, Blocking: false, RecommendedAnswer: "keep it"}
	assert.NoError(t, nonBlockingWithDefault.Validate())

	emptyPrompt := domain.NewQuestionInput{Prompt: "  ", Kind: domain.QuestionKindTechnical, Blocking: true}
	assert.ErrorIs(t, emptyPrompt.Validate(), domain.ErrQuestionInvalid)

	badKind := domain.NewQuestionInput{Prompt: "x", Kind: "ux", Blocking: true}
	assert.ErrorIs(t, badKind.Validate(), domain.ErrQuestionInvalid)

	tooLong := domain.NewQuestionInput{Prompt: strings.Repeat("x", domain.MaxQuestionPromptChars+1), Kind: domain.QuestionKindTechnical, Blocking: true}
	assert.ErrorIs(t, tooLong.Validate(), domain.ErrQuestionInvalid)
}

func TestApplyQuestionUpdateRefusesChangingAnAnsweredPrompt(t *testing.T) {
	answered := domain.TaskQuestion{Key: "Q1", Prompt: "Which queue?", Blocking: true, Status: domain.QuestionStatusAnswered, Answer: "SQS"}
	newPrompt := "Which queue should the worker use?"

	_, err := domain.ApplyQuestionUpdate(answered, domain.UpdateQuestionInput{Key: "Q1", Prompt: &newPrompt})
	assert.ErrorIs(t, err, domain.ErrQuestionConflict)

	samePrompt := answered.Prompt
	updated, err := domain.ApplyQuestionUpdate(answered, domain.UpdateQuestionInput{Key: "Q1", Prompt: &samePrompt})
	assert.NoError(t, err, "re-sending the identical wording is not a change")
	assert.Equal(t, answered.Prompt, updated.Prompt)
}

func TestApplyQuestionUpdateRefusesEditingAWithdrawnQuestion(t *testing.T) {
	withdrawn := domain.TaskQuestion{Key: "Q1", Status: domain.QuestionStatusWithdrawn}
	blocking := true
	_, err := domain.ApplyQuestionUpdate(withdrawn, domain.UpdateQuestionInput{Key: "Q1", Blocking: &blocking})
	assert.ErrorIs(t, err, domain.ErrQuestionConflict)
}

func TestApplyQuestionUpdateCanEditAnOpenQuestion(t *testing.T) {
	open := domain.TaskQuestion{Key: "Q1", Prompt: "Which queue?", Blocking: true, Status: domain.QuestionStatusOpen}
	blocking := false
	answer := "Keep it as-is."
	updated, err := domain.ApplyQuestionUpdate(open, domain.UpdateQuestionInput{Key: "Q1", Blocking: &blocking, RecommendedAnswer: &answer})
	require.NoError(t, err)
	assert.False(t, updated.Blocking)
	assert.Equal(t, "Keep it as-is.", updated.RecommendedAnswer)

	blockingAgain := true
	_, err = domain.ApplyQuestionUpdate(updated, domain.UpdateQuestionInput{Key: "Q1", Blocking: &blockingAgain, RecommendedAnswer: new(string)})
	assert.NoError(t, err, "turning blocking back on with an empty recommended answer is fine — blocking needs none")
}

func TestApplyQuestionAnswer(t *testing.T) {
	q := domain.TaskQuestion{Key: "Q1", Status: domain.QuestionStatusOpen}
	now := time.Now().UTC()

	answered, err := domain.ApplyQuestionAnswer(q, "  Use SQS.  ", now)
	require.NoError(t, err)
	assert.Equal(t, domain.QuestionStatusAnswered, answered.Status)
	assert.Equal(t, "Use SQS.", answered.Answer)
	require.NotNil(t, answered.AnsweredAt)

	reopened, err := domain.ApplyQuestionAnswer(answered, "", now)
	require.NoError(t, err)
	assert.Equal(t, domain.QuestionStatusOpen, reopened.Status)
	assert.Nil(t, reopened.AnsweredAt)
	assert.Nil(t, reopened.SubmittedAt)

	withdrawn := domain.TaskQuestion{Key: "Q1", Status: domain.QuestionStatusWithdrawn}
	_, err = domain.ApplyQuestionAnswer(withdrawn, "x", now)
	assert.ErrorIs(t, err, domain.ErrQuestionConflict)

	tooLong := strings.Repeat("x", domain.MaxQuestionAnswerChars+1)
	_, err = domain.ApplyQuestionAnswer(q, tooLong, now)
	assert.ErrorIs(t, err, domain.ErrQuestionInvalid)
}

func TestPendingBlockingKeys(t *testing.T) {
	items := []domain.TaskQuestion{
		{Key: "Q1", Blocking: true, Status: domain.QuestionStatusOpen},
		{Key: "Q2", Blocking: true, Status: domain.QuestionStatusAnswered},
		{Key: "Q3", Blocking: false, Status: domain.QuestionStatusOpen},
		{Key: "Q4", Blocking: true, Status: domain.QuestionStatusWithdrawn},
	}
	assert.Equal(t, []string{"Q1"}, domain.PendingBlockingKeys(items))
}

func TestAnsweredUnsubmitted(t *testing.T) {
	submittedAt := time.Now()
	items := []domain.TaskQuestion{
		{Key: "Q1", Status: domain.QuestionStatusAnswered},
		{Key: "Q2", Status: domain.QuestionStatusAnswered, SubmittedAt: &submittedAt},
		{Key: "Q3", Status: domain.QuestionStatusOpen},
	}
	got := domain.AnsweredUnsubmitted(items)
	require.Len(t, got, 1)
	assert.Equal(t, "Q1", got[0].Key)
}

func TestNonWithdrawn(t *testing.T) {
	items := []domain.TaskQuestion{
		{Key: "Q1", Status: domain.QuestionStatusOpen},
		{Key: "Q2", Status: domain.QuestionStatusWithdrawn},
	}
	got := domain.NonWithdrawn(items)
	require.Len(t, got, 1)
	assert.Equal(t, "Q1", got[0].Key)
}
