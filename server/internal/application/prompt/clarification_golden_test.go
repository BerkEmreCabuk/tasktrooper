package prompt_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestGoldenClarificationStaticGuidance(t *testing.T) {
	assertGolden(t, "clarification_guidance", prompt.ClarificationGuidance())
	assertGolden(t, "cli_clarification_guidance", prompt.CLIClarificationGuidance())
	assertGolden(t, "ask_user_task_guidance", prompt.AskUserTaskGuidance())
}

func clarificationFixtures() map[string]domain.ClarificationRequest {
	return map[string]domain.ClarificationRequest{
		"with_options": {
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
		},
		"no_options_no_context": {
			Questions: []domain.ClarificationQuestion{
				{ID: "q1", Prompt: "Which environment?"},
			},
		},
		"multiple_questions_mixed": {
			Context: "A few things before I continue.",
			Questions: []domain.ClarificationQuestion{
				{ID: "q1", Prompt: "First question?"},
				{
					ID:     "q2",
					Prompt: "Second question with choices?",
					Options: []domain.ClarificationOption{
						{ID: "a", Label: "Option A"},
						{ID: "b", Label: "Option B"},
						{ID: "c", Label: "Option C"},
					},
				},
			},
		},
	}
}

func TestGoldenFormatClarificationMessage(t *testing.T) {
	for name, req := range clarificationFixtures() {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, "format_clarification_message_"+name, prompt.FormatClarificationMessage(req))
		})
	}
}

func TestGoldenFormatClarificationQuestions(t *testing.T) {
	for name, req := range clarificationFixtures() {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, "format_clarification_questions_"+name, prompt.FormatClarificationQuestions(req))
		})
	}
}

func TestGoldenClarificationHistoryNote(t *testing.T) {
	for name, req := range clarificationFixtures() {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, "clarification_history_note_"+name, prompt.ClarificationHistoryNote(req))
		})
	}
	assertGolden(t, "clarification_history_note_empty", prompt.ClarificationHistoryNote(domain.ClarificationRequest{}))
}

func TestGoldenAnsweredClarificationsMessage(t *testing.T) {
	single := []domain.TaskComment{
		{Content: prompt.ClarificationAnswerComment("Where should the link go?", "In the footer.")},
	}
	assertGolden(t, "answered_clarifications_single", prompt.AnsweredClarificationsMessage(single))

	withUnrelated := []domain.TaskComment{
		{Content: "unrelated reviewer note"},
		{Content: prompt.ClarificationAnswerComment("What should the label be?", "Get it on Google Play")},
	}
	assertGolden(t, "answered_clarifications_with_unrelated", prompt.AnsweredClarificationsMessage(withUnrelated))

	many := make([]domain.TaskComment, 0, 14)
	for i := range 14 {
		many = append(many, domain.TaskComment{
			Content: prompt.ClarificationAnswerComment(fmt.Sprintf("question %d", i), fmt.Sprintf("answer %d", i)),
		})
	}
	assertGolden(t, "answered_clarifications_omits_older", prompt.AnsweredClarificationsMessage(many))

	truncated := []domain.TaskComment{
		{Content: prompt.ClarificationAnswerComment("q", strings.Repeat("z", 6000))},
	}
	assertGolden(t, "answered_clarifications_truncated", prompt.AnsweredClarificationsMessage(truncated))
}

func TestGoldenClarificationAckMessage(t *testing.T) {
	assertGolden(t, "clarification_ack_en", prompt.ClarificationAckMessage("en"))
	assertGolden(t, "clarification_ack_tr", prompt.ClarificationAckMessage("tr"))
	assertGolden(t, "clarification_ack_default", prompt.ClarificationAckMessage("fr"))
}

func TestGoldenUserFacingLanguageRule(t *testing.T) {
	assertGolden(t, "user_facing_language_rule_en", prompt.UserFacingLanguageRule("en"))
	assertGolden(t, "user_facing_language_rule_tr", prompt.UserFacingLanguageRule("tr"))
	assertGolden(t, "user_facing_language_rule_default", prompt.UserFacingLanguageRule(""))
}
