package board

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// An acceptance criterion states what the DELIVERABLE does once the work is
// finished — something a reviewer can observe in the product. Agents kept
// writing the workflow around the card instead: "moved to analiz_review",
// "implementation tasks created", "spec attached with add_task_document". Those
// are board mechanics, and as criteria they are actively harmful:
//
//   - They can only be ticked by doing the very hand-off that ends the task, so
//     the card cannot legally reach done — the criteria gate holds it while the
//     agent waits for a criterion it will never be allowed to complete.
//   - They pass review while saying nothing about whether the work is any good:
//     a spec that got attached is not a spec that answers the question.
//   - The analiz case the rule was written for: "implementation tasks are
//     opened" happens AFTER a human approves the analysis, so making it a
//     criterion of the analysis inverts the order of the whole flow.
//
// So the criteria written by agents are filtered here, at the one place both
// create_board_task and update_board_task pass through. Criteria a human types
// in the UI are never touched — this is about what the agents generate.
//
// The patterns below are deliberately about the card's own journey ("moved to
// X", "task is created", "attached with add_task_document"). Product behaviour
// that happens to involve tasks and columns — this app's own board features are
// exactly that — is left alone: it does not read as an instruction to move,
// open or hand off the card being written.

type droppedCriterion struct {
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

type criterionRule struct {
	pattern *regexp.Regexp
	reason  prompt.Key[struct{}]
}

// Column names are matched as words so "the done column" and "ready_for_qa"
// both hit, while "done" on its own ("Then the import is done") does not.
const columnAlternatives = `(?:backlog|todo|in[_ ]progress|code[_ ]review|ready[_ ]for[_ ]qa|in[_ ]qa|pm[_ ]uat|analiz[_ ]review|need[_ ]revision|blocked|done|released)`

var criterionRules = []criterionRule{
	{
		// "…is moved to ready_for_qa", "advance the card into done column"
		pattern: regexp.MustCompile(`(?i)\b(?:move[sd]?|moving|transition(?:s|ed)?|advance[sd]?|promote[sd]?|place[sd]?|put)\b[^.;]{0,40}?\b(?:to|into)\b\s+(?:the\s+)?(?:` + columnAlternatives + `\b|[a-z_ ]{0,20}\bcolumn\b)`),
		reason:  criterionRuleCardMoveKey,
	},
	{
		// "…sits in analiz_review for approval", "left in the done column"
		pattern: regexp.MustCompile(`(?i)\b(?:sits?|sitting|left|waits?|waiting|stays?|remains?|presented|submitted|handed)\b[^.;]{0,40}?\b(?:in|to|for)\b[^.;]{0,30}?(?:\b` + columnAlternatives + `\b|\bcolumn\b|\b(?:human\s+)?approval\b)`),
		reason:  criterionRuleCardWaitingKey,
	},
	{
		// "implementation tasks are created", "sub-tasks opened for each unit"
		pattern: regexp.MustCompile(`(?i)\b(?:implementation|follow[- ]?up|child|sub[- ]?|new)\s*tasks?\b[^.;]{0,40}?\b(?:created|opened|raised|filed|split)\b`),
		reason:  criterionRuleNextTasksKey,
	},
	{
		// "create implementation tasks for each unit"
		pattern: regexp.MustCompile(`(?i)\b(?:creates?|created|opens?|opened|raises?|files?)\b[^.;]{0,20}?\b(?:implementation|follow[- ]?up|child|sub[- ]?)\s*tasks?\b`),
		reason:  criterionRuleNextTasksKey,
	},
	{
		// "…attached with add_task_document", "recorded via add_task_comment"
		pattern: regexp.MustCompile(`(?i)\b(?:with|via|using|through|by(?:\s+calling)?)\s+(?:the\s+)?(?:create_board_task|update_board_task|move_board_task|claim_board_task|delete_board_task|add_task_comment|add_task_document|update_task_document|attach_task_file|set_criterion_completed|review_criterion|list_acceptance_criteria|trigger_release|commit_task_changes|comment_on_pull_request)\b`),
		reason:  criterionRuleToolMechanicsKey,
	},
	{
		// "the task is assigned to the system-architect"
		pattern: regexp.MustCompile(`(?i)\b(?:assigned|reassigned|handed\s+off|dispatched)\b[^.;]{0,20}?\bto\b\s+(?:the\s+)?(?:system-architect|product-manager|qa-agent|architect|developer|implementer|agent|human|stakeholder)\b`),
		reason:  criterionRuleHandoffKey,
	},
}

// parkStateDescription exempts a legitimate product-behaviour criterion —
// this app's own park feature leaves the underlying task blocked WITH A
// REASON — from the column-move rule above. "put into blocked" alone reads
// exactly like a literal move instruction, but paired with "reason" nearby it
// is describing what the deliverable does to some other task, not moving
// THIS one. Go's RE2 has no lookaround, so this has to be a second pattern
// checked ahead of criterionRules rather than a negative lookahead inline.
var parkStateDescription = regexp.MustCompile(`(?i)\bblocked\b[^.;]{0,40}?\breason\b|\breason\b[^.;]{0,40}?\bblocked\b`)

// boardActionReason reports why a criterion is board workflow rather than an
// observable property of the work, or "" when it is a legitimate criterion.
func boardActionReason(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if parkStateDescription.MatchString(trimmed) {
		return ""
	}
	for _, rule := range criterionRules {
		if rule.pattern.MatchString(trimmed) {
			return prompt.Text(rule.reason)
		}
	}
	return ""
}

// criteriaHint is appended to a tool result whenever something was dropped, so
// the agent learns the rule from the failure instead of repeating it.
func criteriaHint(dropped []droppedCriterion) string {
	return criteriaDroppedHintKey.Render(countInput{Count: len(dropped)})
}
