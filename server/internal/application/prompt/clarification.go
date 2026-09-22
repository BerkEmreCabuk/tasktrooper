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

// ask_user is refused over MCP before any policy filtering (it parks a task in a way a live CLI session cannot offer), so this says the opposite of clarificationGuidance: no ask_user, and the questions live in the closing message the runner already surfaces on the card.
const cliClarificationGuidance = `## Missing information
- Never assume missing requirements, scope, preferences, or constraints.
- Read the repository before you call something unknown: file layout, where a section or component lives, routing and existing config are yours to find, not the human's to describe.
- You cannot reach the human mid-run — this session has no way to wait for an answer. Where the choice is reversible, decide with what you have and say what you assumed. Where it is not, stop and state in your closing message what is missing and what you would need; that message is shown on the task card.
- Read the task's comments first. A question already answered there is decided, not open.`

func CLIClarificationGuidance() string {
	return cliClarificationGuidance
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

// The stored assistant message only held the context line; the questions lived in a JSONB column the model never saw, so the next turn read the answer without knowing what it answered.
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

// A parked task once remembered only the context line, which is not a question; the question text is the part that must survive.
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
	var sb strings.Builder
	sb.WriteString("## Clarifications already answered on this task\n")
	sb.WriteString("The human has already answered the questions below. Treat these answers as decided requirements, ")
	sb.WriteString("act on them, and never ask them again — only ask about something genuinely still unknown.\n")
	if omitted > 0 {
		// Said out loud, so a run that finds a gap looks it up instead of deciding the question was never answered.
		fmt.Fprintf(&sb, "The %d most recent are shown; %d older answer(s) are on this task's comments — read them with list_comments before treating anything as unanswered.\n",
			len(answered), omitted)
	}
	for _, a := range answered {
		body := domain.TruncateHead(a, maxAnsweredClarificationChars)
		if len(body) < len(a) {
			body += "…"
		}
		sb.WriteString("\n")
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

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
