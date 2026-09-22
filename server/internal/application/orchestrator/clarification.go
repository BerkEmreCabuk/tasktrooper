package orchestrator

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

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
		return fmt.Errorf("provide at least one clarification question")
	}
	for _, q := range questions {
		if q.ID == "" {
			return fmt.Errorf("clarification question (prompt=%q) is missing its \"id\"", q.Prompt)
		}
		if q.Prompt == "" {
			return fmt.Errorf("clarification question %q is missing its \"prompt\" text", q.ID)
		}
		if len(q.Options) < 2 {
			return fmt.Errorf("clarification question %q needs at least two \"options\"", q.ID)
		}
		for _, o := range q.Options {
			if o.ID == "" || o.Label == "" {
				return fmt.Errorf("clarification question %q has an option missing \"id\" or \"label\"", q.ID)
			}
		}
	}
	return domain.ValidateClarificationQuestions(questions)
}

func clarificationStepPayload(req domain.ClarificationRequest, source string) map[string]any {
	return domain.ClarificationStepPayload(req, source)
}
