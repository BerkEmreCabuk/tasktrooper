package prompt_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func storeLinkRequest() domain.ClarificationRequest {
	return domain.ClarificationRequest{
		Context: "I need to confirm the placement and label for the link.",
		Questions: []domain.ClarificationQuestion{
			{
				ID:     "placement",
				Prompt: "Where should the Google Play Store link be placed?",
				Options: []domain.ClarificationOption{
					{ID: "landing", Label: "Landing page"},
					{ID: "footer", Label: "Footer"},
				},
			},
			{ID: "label", Prompt: "What should the label be?"},
		},
	}
}

// The parked question has to carry the questions themselves, not just the
// context line — that is what the resumed run reads to know what was answered.
func TestFormatClarificationQuestions_KeepsContextAndQuestions(t *testing.T) {
	out := prompt.FormatClarificationQuestions(storeLinkRequest())

	assert.Contains(t, out, "I need to confirm the placement and label for the link.")
	assert.Contains(t, out, "1. Where should the Google Play Store link be placed?")
	assert.Contains(t, out, "[options: Landing page | Footer]")
	assert.Contains(t, out, "2. What should the label be?")
}

func TestAnsweredClarificationsMessage_ReplaysAnsweredExchanges(t *testing.T) {
	comment := prompt.ClarificationAnswerComment(
		prompt.FormatClarificationQuestions(storeLinkRequest()),
		"Where should the Google Play Store link be placed?: Both\nWhat should the label be?: Get it on Google Play",
	)
	assert.True(t, prompt.IsClarificationComment(comment))

	msg := prompt.AnsweredClarificationsMessage([]domain.TaskComment{
		{Content: "unrelated reviewer note"},
		{Content: comment},
	})

	assert.Contains(t, msg, "never ask them again")
	assert.Contains(t, msg, "Where should the Google Play Store link be placed?")
	assert.Contains(t, msg, "Get it on Google Play")
	assert.NotContains(t, msg, "unrelated reviewer note")
	assert.NotContains(t, msg, prompt.ClarificationCommentPrefix)
}

// A task with no answered question must not get an empty header injected into
// every run's context.
func TestAnsweredClarificationsMessage_EmptyWithoutClarifications(t *testing.T) {
	assert.Empty(t, prompt.AnsweredClarificationsMessage(nil))
	assert.Empty(t, prompt.AnsweredClarificationsMessage([]domain.TaskComment{{Content: "plain comment"}}))
}

// The block rides into every run of the task and the comments behind it are
// unbounded, so it cannot grow with the card's whole clarification history.
func TestAnsweredClarificationsMessage_KeepsNewestAndSaysWhatItDropped(t *testing.T) {
	comments := make([]domain.TaskComment, 0, 14)
	for i := range 14 {
		comments = append(comments, domain.TaskComment{
			Content: prompt.ClarificationAnswerComment(fmt.Sprintf("question %d", i), fmt.Sprintf("answer %d", i)),
		})
	}

	msg := prompt.AnsweredClarificationsMessage(comments)

	assert.Contains(t, msg, "answer 13", "the newest answer is the one still being acted on")
	assert.Contains(t, msg, "answer 4")
	assert.NotContains(t, msg, "answer 3", "answers past the cap are dropped oldest first")
	// A gap the run cannot see is worse than a gap it is told about.
	assert.Contains(t, msg, "4 older")
	assert.Contains(t, msg, "list_comments")
}

// One pathological answer must not undo the cap.
func TestAnsweredClarificationsMessage_TruncatesALongAnswer(t *testing.T) {
	msg := prompt.AnsweredClarificationsMessage([]domain.TaskComment{
		{Content: prompt.ClarificationAnswerComment("q", strings.Repeat("z", 6000))},
	})

	assert.Less(t, len(msg), 3000)
	assert.Contains(t, msg, "…")
}

func TestUserFacingLanguageRule_TurkishLocale(t *testing.T) {
	rule := prompt.UserFacingLanguageRule("tr")
	assert.Contains(t, rule, "Turkish")
}

func TestUserFacingLanguageRule_EnglishLocale(t *testing.T) {
	rule := prompt.UserFacingLanguageRule("en")
	assert.Contains(t, rule, "English")
}
