package orchestrator

import (
	"errors"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type clarificationPromptData struct{ QuotedPrompt string }
type clarificationIDData struct{ QuotedID string }
type clarificationQuestionIDData struct{ QuestionID string }
type clarificationReservedOptionData struct{ QuestionID, OptionID string }

var (
	clarificationNoQuestionsKey        = prompt.Define[struct{}]("guard.clarification_no_questions", struct{}{})
	clarificationMissingIDKey          = prompt.Define("guard.clarification_missing_id", clarificationPromptData{QuotedPrompt: `"sample"`})
	clarificationMissingPromptKey      = prompt.Define("guard.clarification_missing_prompt", clarificationIDData{QuotedID: `"sample"`})
	clarificationTooFewOptionsKey      = prompt.Define("guard.clarification_too_few_options", clarificationIDData{QuotedID: `"sample"`})
	clarificationOptionMissingLabelKey = prompt.Define("guard.clarification_option_missing_label", clarificationIDData{QuotedID: `"sample"`})

	clarificationChoiceMissingOtherKey   = prompt.Define("guard.clarification_choice_missing_other", clarificationQuestionIDData{QuestionID: "sample"})
	clarificationChoiceReservedOptionKey = prompt.Define("guard.clarification_choice_reserved_option", clarificationReservedOptionData{QuestionID: "sample", OptionID: "skip"})
	clarificationChoiceNeedsConcreteKey  = prompt.Define("guard.clarification_choice_needs_concrete", clarificationQuestionIDData{QuestionID: "sample"})
	clarificationTextModeExtraOptionKey  = prompt.Define("guard.clarification_text_mode_extra_option", clarificationQuestionIDData{QuestionID: "sample"})
	clarificationDomainTooFewOptionsKey  = prompt.Define("guard.clarification_domain_too_few_options", clarificationQuestionIDData{QuestionID: "sample"})
)

// renderClarificationRuleError turns domain's structured validation failure
// into the wording the model retries against; domain itself carries no
// authored prose (see domain.ClarificationRuleError).
func renderClarificationRuleError(err error) error {
	var ruleErr *domain.ClarificationRuleError
	if !errors.As(err, &ruleErr) {
		return err
	}
	switch ruleErr.Code {
	case domain.ClarificationRuleTooFewOptions:
		return errors.New(clarificationDomainTooFewOptionsKey.Render(clarificationQuestionIDData{QuestionID: ruleErr.QuestionID}))
	case domain.ClarificationRuleTextModeExtraOption:
		return errors.New(clarificationTextModeExtraOptionKey.Render(clarificationQuestionIDData{QuestionID: ruleErr.QuestionID}))
	case domain.ClarificationRuleChoiceMissingOther:
		return errors.New(clarificationChoiceMissingOtherKey.Render(clarificationQuestionIDData{QuestionID: ruleErr.QuestionID}))
	case domain.ClarificationRuleChoiceReservedOption:
		return errors.New(clarificationChoiceReservedOptionKey.Render(clarificationReservedOptionData{QuestionID: ruleErr.QuestionID, OptionID: ruleErr.OptionID}))
	case domain.ClarificationRuleChoiceNeedsConcrete:
		return errors.New(clarificationChoiceNeedsConcreteKey.Render(clarificationQuestionIDData{QuestionID: ruleErr.QuestionID}))
	default:
		return err
	}
}

type ClarificationNeededError struct {
	Request domain.ClarificationRequest
}

func (e ClarificationNeededError) Error() string {
	return "clarification needed"
}

func clarificationFromIntake(intake domain.GoalIntake) domain.ClarificationRequest {
	ctx := intake.Purpose
	if ctx == "" {
		ctx = "I need a few details before I can continue."
	}
	return domain.ClarificationRequest{
		Context:   ctx,
		Questions: intake.Questions,
	}
}

func clarificationFromPlanner(output domain.PlannerOutput) domain.ClarificationRequest {
	ctx := output.Summary
	if ctx == "" {
		ctx = "I need a few details before I can create the plan."
	}
	return domain.ClarificationRequest{
		Context:   ctx,
		Questions: output.Questions,
	}
}

func buildClarificationResponse(req domain.ClarificationRequest) domain.AgentResponse {
	return prompt.BuildClarificationResponse(req)
}

func validateClarificationQuestions(questions []domain.ClarificationQuestion) error {
	// The specific violation is fed back to the model as a self-correction hint.
	if len(questions) == 0 {
		return errors.New(prompt.Text(clarificationNoQuestionsKey))
	}
	for _, q := range questions {
		if q.ID == "" {
			return errors.New(clarificationMissingIDKey.Render(clarificationPromptData{QuotedPrompt: fmt.Sprintf("%q", q.Prompt)}))
		}
		if q.Prompt == "" {
			return errors.New(clarificationMissingPromptKey.Render(clarificationIDData{QuotedID: fmt.Sprintf("%q", q.ID)}))
		}
		if len(q.Options) < 2 {
			return errors.New(clarificationTooFewOptionsKey.Render(clarificationIDData{QuotedID: fmt.Sprintf("%q", q.ID)}))
		}
		for _, o := range q.Options {
			if o.ID == "" || o.Label == "" {
				return errors.New(clarificationOptionMissingLabelKey.Render(clarificationIDData{QuotedID: fmt.Sprintf("%q", q.ID)}))
			}
		}
	}
	if err := domain.ValidateClarificationQuestions(questions); err != nil {
		return renderClarificationRuleError(err)
	}
	return nil
}

func clarificationStepPayload(req domain.ClarificationRequest, source string) map[string]any {
	return domain.ClarificationStepPayload(req, source)
}
