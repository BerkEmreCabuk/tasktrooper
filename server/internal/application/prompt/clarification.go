package prompt

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const clarificationGuidance = `## Clarification policy
- Never assume missing requirements, scope, preferences, or constraints.
- Read the repository before you ask about it: file layout, where a section or component lives, routing and existing config are yours to find, not the human's to describe.
- If anything is still unclear after looking, call ask_user before proceeding — follow the ask_user tool definition (text mode vs choice mode).
- ask_user context, prompt, and option labels must be in the user-facing language (see language instruction).
- Do not write clarification questions in your message body.
- Read the thread first; do not repeat answered questions.
- Only continue after the user submits clarification answers.`

var clarificationGuidanceKey = Define[struct{}]("clarification.guidance", struct{}{})

func ClarificationGuidance() string {
	return Text(clarificationGuidanceKey)
}

// ask_user is refused over MCP before any policy filtering (it parks a task in a way a live CLI session cannot offer), so this says the opposite of clarificationGuidance: no ask_user, and the questions live in the closing message the runner already surfaces on the card.
const cliClarificationGuidance = `## Missing information
- Never assume missing requirements, scope, preferences, or constraints.
- Read the repository before you call something unknown: file layout, where a section or component lives, routing and existing config are yours to find, not the human's to describe.
- You cannot reach the human mid-run — this session has no way to wait for an answer. Where the choice is reversible, decide with what you have and say what you assumed. Where it is not, stop and state in your closing message what is missing and what you would need; that message is shown on the task card.
- Read the task's comments first. A question already answered there is decided, not open.`

var cliClarificationGuidanceKey = Define[struct{}]("clarification.cli_guidance", struct{}{})

func CLIClarificationGuidance() string {
	return Text(cliClarificationGuidanceKey)
}

type clarificationQuestionBlock struct {
	Number  int
	Prompt  string
	Options []string
}

type formatClarificationMessageInput struct {
	Context   string
	Questions []clarificationQuestionBlock
}

var formatClarificationMessageKey = Define("clarification.format_message", formatClarificationMessageInput{
	Context:   "Sample context.",
	Questions: []clarificationQuestionBlock{{Number: 1, Prompt: "Sample question?", Options: []string{"A", "B"}}},
})

func FormatClarificationMessage(req domain.ClarificationRequest) string {
	blocks := make([]clarificationQuestionBlock, 0, len(req.Questions))
	for i, q := range req.Questions {
		blocks = append(blocks, clarificationQuestionBlock{Number: i + 1, Prompt: q.Prompt, Options: optionLabels(q.Options)})
	}
	return strings.TrimSpace(formatClarificationMessageKey.Render(formatClarificationMessageInput{
		Context:   req.Context,
		Questions: blocks,
	}))
}

func optionLabels(options []domain.ClarificationOption) []string {
	labels := make([]string, 0, len(options))
	for _, opt := range options {
		labels = append(labels, opt.Label)
	}
	return labels
}

// The stored assistant message only held the context line; the questions lived in a JSONB column the model never saw, so the next turn read the answer without knowing what it answered.
type historyNoteInput struct {
	Lines []string
}

var historyNoteKey = Define("clarification.history_note", historyNoteInput{Lines: []string{"1. Sample question?"}})

func ClarificationHistoryNote(req domain.ClarificationRequest) string {
	if len(req.Questions) == 0 {
		return ""
	}
	lines := make([]string, len(req.Questions))
	for i, q := range req.Questions {
		lines[i] = numberedQuestion(i, q)
	}
	return historyNoteKey.Render(historyNoteInput{Lines: lines})
}

type formatQuestionsInput struct {
	Parts []string
}

var formatQuestionsKey = Define("clarification.format_questions", formatQuestionsInput{
	Parts: []string{"Sample context.", "1. Sample question?"},
})

// A parked task once remembered only the context line, which is not a question; the question text is the part that must survive.
func FormatClarificationQuestions(req domain.ClarificationRequest) string {
	var parts []string
	if ctx := strings.TrimSpace(req.Context); ctx != "" {
		parts = append(parts, ctx)
	}
	for i, q := range req.Questions {
		parts = append(parts, numberedQuestion(i, q))
	}
	return formatQuestionsKey.Render(formatQuestionsInput{Parts: parts})
}

func numberedQuestion(index int, q domain.ClarificationQuestion) string {
	line := fmt.Sprintf("%d. %s", index+1, q.Prompt)
	if len(q.Options) == 0 {
		return line
	}
	labels := make([]string, 0, len(q.Options))
	for _, opt := range q.Options {
		labels = append(labels, opt.Label)
	}
	return line + " [options: " + strings.Join(labels, " | ") + "]"
}

// Answers used to live only in the throwaway chat session, so the task remembered nothing and the next run asked again; the comment is that memory and the prefix is how a later run finds it.
const ClarificationCommentPrefix = "[clarification]"

func ClarificationAnswerComment(question, answer string) string {
	var sb strings.Builder
	sb.WriteString(ClarificationCommentPrefix)
	if q := strings.TrimSpace(question); q != "" {
		sb.WriteString("\nAsked:\n")
		sb.WriteString(q)
	}
	sb.WriteString("\nAnswered by the human:\n")
	sb.WriteString(strings.TrimSpace(answer))
	return sb.String()
}

func IsClarificationComment(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), ClarificationCommentPrefix)
}

// Everything behind this is uncapped (ListByTask carries no LIMIT) and goes into every run, so a long-lived card would replay its whole clarification history on each dispatch.
const (
	maxAnsweredClarifications     = 10
	maxAnsweredClarificationChars = 2000
)

type answeredInput struct {
	ShownCount   int
	OmittedCount int
	Answers      []string
}

var answeredKey = Define("clarification.answered", answeredInput{
	ShownCount: 1,
	Answers:    []string{"Asked:\nSample question?\nAnswered by the human:\nSample answer."},
})

func AnsweredClarificationsMessage(comments []domain.TaskComment) string {
	answered := make([]string, 0, len(comments))
	for _, c := range comments {
		if !IsClarificationComment(c.Content) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Content), ClarificationCommentPrefix))
		if body != "" {
			answered = append(answered, body)
		}
	}
	if len(answered) == 0 {
		return ""
	}
	omitted := 0
	if len(answered) > maxAnsweredClarifications {
		omitted = len(answered) - maxAnsweredClarifications
		answered = answered[omitted:]
	}
	shown := make([]string, len(answered))
	for i, a := range answered {
		body := domain.TruncateHead(a, maxAnsweredClarificationChars)
		if len(body) < len(a) {
			body += "…"
		}
		shown[i] = body
	}
	return strings.TrimSpace(answeredKey.Render(answeredInput{
		ShownCount:   len(shown),
		OmittedCount: omitted,
		Answers:      shown,
	}))
}

type ackInput struct {
	Lang string
}

var ackKey = Define("clarification.ack", ackInput{Lang: "en"})

func ClarificationAckMessage(lang string) string {
	return ackKey.Render(ackInput{Lang: lang})
}

func BuildClarificationResponse(req domain.ClarificationRequest) domain.AgentResponse {
	content := req.Context
	if content == "" {
		content = "I need a few details before I can continue."
	}
	reqCopy := req
	return domain.AgentResponse{
		Message:       domain.Message{Role: domain.RoleAssistant, Content: content},
		Clarification: &reqCopy,
	}
}

const askUserTaskGuidance = `## Internal: user clarification
If you need information from the user, call ask_user — follow its tool definition (text mode vs choice mode, no-repeat rule).
Anything the repository can answer (file layout, where a page or component lives, routing, existing config) you must find with your read tools first; ask_user is refused until this run has read the code.
Never write clarification questions as markdown in your reply.`

var askUserTaskGuidanceKey = Define[struct{}]("clarification.ask_user_task_guidance", struct{}{})

func AskUserTaskGuidance() string {
	return Text(askUserTaskGuidanceKey)
}

type userFacingLanguageRuleInput struct {
	Locale string
}

var userFacingLanguageRuleKey = Define("clarification.user_facing_language_rule", userFacingLanguageRuleInput{Locale: "English"})

func UserFacingLanguageRule(lang string) string {
	return userFacingLanguageRuleKey.Render(userFacingLanguageRuleInput{Locale: LocaleDisplayName(lang)})
}
