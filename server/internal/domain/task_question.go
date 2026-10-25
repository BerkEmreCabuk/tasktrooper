package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type QuestionKind string

const (
	QuestionKindProduct   QuestionKind = "product"
	QuestionKindTechnical QuestionKind = "technical"
)

func ValidQuestionKind(k QuestionKind) bool {
	switch k {
	case QuestionKindProduct, QuestionKindTechnical:
		return true
	}
	return false
}

type QuestionStatus string

const (
	QuestionStatusOpen      QuestionStatus = "open"
	QuestionStatusAnswered  QuestionStatus = "answered"
	QuestionStatusWithdrawn QuestionStatus = "withdrawn"
)

func ValidQuestionStatus(s QuestionStatus) bool {
	switch s {
	case QuestionStatusOpen, QuestionStatusAnswered, QuestionStatusWithdrawn:
		return true
	}
	return false
}

// Limits are in characters (runes) — the unit a human writing a question or
// an answer sees.
const (
	MaxQuestionPromptChars            = 2000
	MaxQuestionRecommendedAnswerChars = 2000
	MaxQuestionAnswerChars            = 4000
	// MaxQuestionsPerTask bounds one record_open_questions call's add list —
	// an analiz run proposing more than this at once is not asking questions,
	// it is outsourcing the analysis.
	MaxQuestionsPerTask = 10
)

var (
	ErrQuestionNotFound = errors.New("task question not found")
	ErrQuestionInvalid  = errors.New("invalid task question")
	// ErrQuestionConflict is a write the question's (or its task's) current
	// state does not allow: answering a withdrawn question, editing the
	// prompt of one the human already answered, submitting while the task is
	// not actually waiting on these questions.
	ErrQuestionConflict = errors.New("task question state conflict")
	// ErrPendingBlockingQuestions is the submit refusal naming which blocking
	// questions still have no answer.
	ErrPendingBlockingQuestions = errors.New("blocking questions are still unanswered")
)

// TaskQuestion is one open question an analiz run recorded for the human,
// with the server-assigned ordinal key ("Q1", "Q2", …) that both the agent
// and the report page address it by.
type TaskQuestion struct {
	ID                uuid.UUID      `json:"id"`
	TaskID            uuid.UUID      `json:"task_id"`
	Key               string         `json:"key"`
	Prompt            string         `json:"prompt"`
	Kind              QuestionKind   `json:"kind"`
	Blocking          bool           `json:"blocking"`
	RecommendedAnswer string         `json:"recommended_answer"`
	Status            QuestionStatus `json:"status"`
	Answer            string         `json:"answer"`
	AnsweredAt        *time.Time     `json:"answered_at,omitempty"`
	SubmittedAt       *time.Time     `json:"submitted_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// PendingBlocking reports whether this question still blocks the analysis: it
// must be answered before the analiz task can leave `blocked`.
func (q TaskQuestion) PendingBlocking() bool {
	return q.Blocking && q.Status == QuestionStatusOpen
}

// NewQuestionInput is one question record_open_questions asks the server to
// add; the key is assigned by the server in creation order.
type NewQuestionInput struct {
	Prompt            string       `json:"prompt"`
	Kind              QuestionKind `json:"kind"`
	Blocking          bool         `json:"blocking"`
	RecommendedAnswer string       `json:"recommended_answer"`
}

// Validate trims Prompt/RecommendedAnswer and enforces the shape every new
// question must have: a non-blocking question with no recommended answer
// would leave the analysis proceeding on nothing at all.
func (in *NewQuestionInput) Validate() error {
	in.Prompt = strings.TrimSpace(in.Prompt)
	in.RecommendedAnswer = strings.TrimSpace(in.RecommendedAnswer)
	if in.Prompt == "" {
		return fmt.Errorf("%w: prompt is required", ErrQuestionInvalid)
	}
	if n := utf8.RuneCountInString(in.Prompt); n > MaxQuestionPromptChars {
		return fmt.Errorf("%w: prompt is %d characters, the limit is %d", ErrQuestionInvalid, n, MaxQuestionPromptChars)
	}
	if !ValidQuestionKind(in.Kind) {
		return fmt.Errorf("%w: kind must be %q or %q", ErrQuestionInvalid, QuestionKindProduct, QuestionKindTechnical)
	}
	if !in.Blocking && in.RecommendedAnswer == "" {
		return fmt.Errorf("%w: a non-blocking question needs a recommended_answer — a blocking one does not, because it has no default to fall back on", ErrQuestionInvalid)
	}
	if n := utf8.RuneCountInString(in.RecommendedAnswer); n > MaxQuestionRecommendedAnswerChars {
		return fmt.Errorf("%w: recommended_answer is %d characters, the limit is %d", ErrQuestionInvalid, n, MaxQuestionRecommendedAnswerChars)
	}
	return nil
}

// UpdateQuestionInput is one {key, ...} entry in record_open_questions'
// `update` list — every field but Key is optional, left alone when nil.
type UpdateQuestionInput struct {
	Key               string
	Prompt            *string
	Kind              *QuestionKind
	Blocking          *bool
	RecommendedAnswer *string
}

// ApplyQuestionUpdate is the agent's half of editing a question it already
// recorded. The prompt may not change once the human has answered it — the
// human answered THAT wording; the agent withdraws and adds a new question
// instead of quietly changing what was asked under them.
func ApplyQuestionUpdate(q TaskQuestion, in UpdateQuestionInput) (TaskQuestion, error) {
	if q.Status == QuestionStatusWithdrawn {
		return q, fmt.Errorf("%w: %s is withdrawn and cannot be edited", ErrQuestionConflict, q.Key)
	}
	if in.Prompt != nil {
		prompt := strings.TrimSpace(*in.Prompt)
		if q.Status == QuestionStatusAnswered && prompt != q.Prompt {
			return q, fmt.Errorf("%w: %s is already answered — withdraw it and record a new question instead of changing the wording the human answered", ErrQuestionConflict, q.Key)
		}
		if prompt == "" {
			return q, fmt.Errorf("%w: prompt is required", ErrQuestionInvalid)
		}
		if n := utf8.RuneCountInString(prompt); n > MaxQuestionPromptChars {
			return q, fmt.Errorf("%w: prompt is %d characters, the limit is %d", ErrQuestionInvalid, n, MaxQuestionPromptChars)
		}
		q.Prompt = prompt
	}
	if in.Kind != nil {
		if !ValidQuestionKind(*in.Kind) {
			return q, fmt.Errorf("%w: kind must be %q or %q", ErrQuestionInvalid, QuestionKindProduct, QuestionKindTechnical)
		}
		q.Kind = *in.Kind
	}
	if in.Blocking != nil {
		q.Blocking = *in.Blocking
	}
	if in.RecommendedAnswer != nil {
		q.RecommendedAnswer = strings.TrimSpace(*in.RecommendedAnswer)
	}
	if !q.Blocking && q.RecommendedAnswer == "" {
		return q, fmt.Errorf("%w: a non-blocking question needs a recommended_answer", ErrQuestionInvalid)
	}
	if n := utf8.RuneCountInString(q.RecommendedAnswer); n > MaxQuestionRecommendedAnswerChars {
		return q, fmt.Errorf("%w: recommended_answer is %d characters, the limit is %d", ErrQuestionInvalid, n, MaxQuestionRecommendedAnswerChars)
	}
	return q, nil
}

// ApplyQuestionAnswer is the human's half: PATCH /questions/:id {answer}. A
// non-empty answer moves the question to answered and stamps answered_at;
// clearing it (back to "") reopens it, matching annotations' reopen rule.
func ApplyQuestionAnswer(q TaskQuestion, answer string, now time.Time) (TaskQuestion, error) {
	if q.Status == QuestionStatusWithdrawn {
		return q, fmt.Errorf("%w: %s is withdrawn and can no longer be answered", ErrQuestionConflict, q.Key)
	}
	answer = strings.TrimSpace(answer)
	if n := utf8.RuneCountInString(answer); n > MaxQuestionAnswerChars {
		return q, fmt.Errorf("%w: answer is %d characters, the limit is %d", ErrQuestionInvalid, n, MaxQuestionAnswerChars)
	}
	q.Answer = answer
	if answer == "" {
		q.Status = QuestionStatusOpen
		q.AnsweredAt = nil
		q.SubmittedAt = nil
		return q, nil
	}
	q.Status = QuestionStatusAnswered
	q.AnsweredAt = &now
	return q, nil
}

// PendingBlockingKeys returns the keys of every blocking question still open,
// in the order given — the 422 submit refusal names exactly these.
func PendingBlockingKeys(items []TaskQuestion) []string {
	var keys []string
	for _, q := range items {
		if q.PendingBlocking() {
			keys = append(keys, q.Key)
		}
	}
	return keys
}

// AnsweredUnsubmitted returns every question that has an answer on record
// but has not yet been told to the agent — the set /questions/submit,
// /annotations/submit and an analiz_review→done approval all stamp
// submitted_at on.
func AnsweredUnsubmitted(items []TaskQuestion) []TaskQuestion {
	var out []TaskQuestion
	for _, q := range items {
		if q.Status == QuestionStatusAnswered && q.SubmittedAt == nil {
			out = append(out, q)
		}
	}
	return out
}

// NonWithdrawn filters out withdrawn questions — the report page and the
// run-context block both show every question except these.
func NonWithdrawn(items []TaskQuestion) []TaskQuestion {
	out := make([]TaskQuestion, 0, len(items))
	for _, q := range items {
		if q.Status != QuestionStatusWithdrawn {
			out = append(out, q)
		}
	}
	return out
}
