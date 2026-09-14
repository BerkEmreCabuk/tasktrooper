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

func ClarificationGuidance() string {
	return clarificationGuidance
}

func FormatClarificationMessage(req domain.ClarificationRequest) string {
	var sb strings.Builder
	sb.WriteString("I need a few details before I can continue:\n\n")
	if req.Context != "" {
		sb.WriteString(req.Context)
		sb.WriteString("\n\n")
	}
	for i, q := range req.Questions {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q.Prompt))
		for _, opt := range q.Options {
			sb.WriteString(fmt.Sprintf("   - %s\n", opt.Label))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Please reply with your choices or additional details.")
	return strings.TrimSpace(sb.String())
}

// ClarificationHistoryNote replays a question the agent already asked back into
// its own history. The stored assistant message only ever held the request's
// context line — the questions themselves lived in a JSONB column the model
// never saw, so on the next turn it read the user's answer without knowing what
// it had answered.
func ClarificationHistoryNote(req domain.ClarificationRequest) string {
	if len(req.Questions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("INTERNAL (never disclose to user): you asked the user the questions below; their reply is the next user message.")
	for i, q := range req.Questions {
		sb.WriteString("\n")
		sb.WriteString(numberedQuestion(i, q))
	}
	return sb.String()
}

// FormatClarificationQuestions renders a request as one readable block: the
// context line the agent wrote, then every question it actually asked.
//
// A parked task used to remember only the context ("I need a few details about
// the store link"), which is not a question — so when the answer came back the
// resumed run was handed a vague preamble paired with a precise reply and asked
// the same questions over again. The question text is the part that has to
// survive.
func FormatClarificationQuestions(req domain.ClarificationRequest) string {
	var sb strings.Builder
	if ctx := strings.TrimSpace(req.Context); ctx != "" {
		sb.WriteString(ctx)
	}
	for i, q := range req.Questions {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(numberedQuestion(i, q))
	}
	return sb.String()
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

// ClarificationCommentPrefix marks the task comment written when a human
// answers an agent's question. Answers used to live only in the throwaway chat
// session the question opened, so the task itself remembered nothing: the next
// run — a resume, a retry, a reviewer hand-back — started blind and asked again.
// The comment is that memory, and the prefix is how a later run finds it.
const ClarificationCommentPrefix = "[clarification]"

// ClarificationAnswerComment renders the answered exchange for the task record.
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

// IsClarificationComment reports a comment written by ClarificationAnswerComment.
func IsClarificationComment(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), ClarificationCommentPrefix)
}

// AnsweredClarificationsMessage replays every question this task already had
// answered into a new run's context. Empty when the task has none.
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
	var sb strings.Builder
	sb.WriteString("## Clarifications already answered on this task\n")
	sb.WriteString("The human has already answered the questions below. Treat these answers as decided requirements, ")
	sb.WriteString("act on them, and never ask them again — only ask about something genuinely still unknown.\n")
	for _, a := range answered {
		sb.WriteString("\n")
		sb.WriteString(a)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// ClarificationAckMessage is what the clarification chat says back once an
// answer has been handed to the task that was waiting on it. Written in the
// user-facing locale, like the questions it replies to.
func ClarificationAckMessage(lang string) string {
	switch lang {
	case "tr":
		return "Cevabın alındı — görev bu cevapla devam ediyor. Başka bir şey net değilse yine buradan sorarım."
	default:
		return "Answer received — the task continues with it. I will ask here if anything else is unclear."
	}
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

func AskUserTaskGuidance() string {
	return askUserTaskGuidance
}

func UserFacingLanguageRule(lang string) string {
	locale := LocaleDisplayName(lang)
	return fmt.Sprintf(`## User-facing language (required)
The configured application locale is %s.
All user-visible strings must be in %s: clarification questions (questions[].prompt), option labels (options[].label), context, and summary.
Internal sub-agent handoffs may use English.`, locale, locale)
}
