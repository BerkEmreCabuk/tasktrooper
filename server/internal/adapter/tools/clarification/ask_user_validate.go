package clarification

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

func validateAskUserQuestions(questions []domain.ClarificationQuestion) error {
	return domain.ValidateClarificationQuestions(questions)
}
