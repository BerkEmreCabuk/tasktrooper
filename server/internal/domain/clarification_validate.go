package domain

import "fmt"

func NormalizeClarificationQuestions(questions []ClarificationQuestion) []ClarificationQuestion {
	out := make([]ClarificationQuestion, len(questions))
	for i, q := range questions {
		out[i] = NormalizeClarificationQuestion(q)
	}
	return out
}

func NormalizeClarificationQuestion(q ClarificationQuestion) ClarificationQuestion {
	if IsTextModeQuestion(q) {
		return q
	}
	for _, opt := range q.Options {
		if opt.ID == "other" {
			return q
		}
	}
	q.Options = append(q.Options, ClarificationOption{ID: "other", Label: "Other"})
	return q
}

func ValidateClarificationQuestions(questions []ClarificationQuestion) error {
	for _, q := range questions {
		if err := ValidateClarificationQuestion(q); err != nil {
			return err
		}
	}
	return nil
}

func ValidateClarificationQuestion(q ClarificationQuestion) error {
	if len(q.Options) < 2 {
		return fmt.Errorf("question %s needs at least two options", q.ID)
	}
	if IsTextModeQuestion(q) {
		for _, opt := range q.Options {
			if opt.ID != "free_text" && opt.ID != "skip" {
				return fmt.Errorf("question %s: text mode allows only free_text and skip options", q.ID)
			}
		}
		return nil
	}
	last := q.Options[len(q.Options)-1]
	if last.ID != "other" {
		return fmt.Errorf("question %s: choice mode requires other as the last option", q.ID)
	}
	concrete := 0
	for i, opt := range q.Options {
		if i == len(q.Options)-1 {
			continue
		}
		if opt.ID == "free_text" || opt.ID == "skip" || opt.ID == "other" {
			return fmt.Errorf("question %s: choice mode must not include %s before other", q.ID, opt.ID)
		}
		concrete++
	}
	if concrete < 1 {
		return fmt.Errorf("question %s: choice mode needs at least one concrete option before other", q.ID)
	}
	return nil
}

func IsTextModeQuestion(q ClarificationQuestion) bool {
	for _, opt := range q.Options {
		if opt.ID == "free_text" {
			return true
		}
	}
	return false
}
