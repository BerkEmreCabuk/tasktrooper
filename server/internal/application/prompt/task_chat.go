package prompt

// The two renderings a task-scoped chat needs: the greeting the human reads when
// the thread opens, and the compact context the model gets on every turn.
//
// They live here rather than in the board or session package because both of
// those need one of them (board opens the thread, session runs its turns) and
// neither may import the other. One file also keeps the two saying the same thing
// about the same task, which is what stops the chat's first line and the model's
// context from disagreeing about the column or the PR.

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// maxTaskChatFieldChars caps each free-text task field. The description and the
// technical notes are written by a PM and an architect with no length limit; the
// per-turn context message pays for them on every single turn, and past this size
// they crowd out the conversation.
const maxTaskChatFieldChars = 2000

// TaskChatOpeningMessage is the assistant's first turn in a task's chat: what the
// task is, where it stands, and the PR if one exists.
//
// It is a chat message, not a system prompt — the human reads it, so it names the
// task the way the board does (key, title, column) and offers the two things they
// most often want next: an explanation of the change, or a change to it.
func TaskChatOpeningMessage(task domain.BoardTask, criteria []domain.AcceptanceCriterion) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s — %s**\n", strings.TrimSpace(task.Key), strings.TrimSpace(task.Title)))
	sb.WriteString(fmt.Sprintf("Column: `%s`", task.Column))
	if task.AssigneeAgentID == nil {
		sb.WriteString(" · unassigned")
	}
	sb.WriteString("\n")
	if desc := strings.TrimSpace(task.Description); desc != "" {
		sb.WriteString("\n" + domain.TruncateHead(desc, maxTaskChatFieldChars) + "\n")
	}
	if len(criteria) > 0 {
		sb.WriteString("\nAcceptance criteria:\n")
		for _, c := range criteria {
			mark := " "
			if c.Completed {
				mark = "x"
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", mark, strings.TrimSpace(c.Text)))
		}
	}
	if url := strings.TrimSpace(task.PRURL); url != "" {
		if task.PRNumber > 0 {
			sb.WriteString(fmt.Sprintf("\nPull request #%d: %s\n", task.PRNumber, url))
		} else {
			sb.WriteString("\nPull request: " + url + "\n")
		}
		sb.WriteString("\nAsk me about the PR — the diff, the changed files, the review comments, whether CI passed — or tell me what to change and I will apply it on the task branch and push, so the PR updates.")
	} else {
		sb.WriteString("\nNo pull request has been opened for this task yet. Tell me what to change and I will apply it on the task branch and push; the PR is opened with the first push.")
	}
	return strings.TrimSpace(sb.String())
}

// TaskChatContextMessage is the per-turn system context for a task-bound chat.
//
// Deliberately cheap: the task's own fields, the branch, and the PR's identity —
// never the diff or the comments. Those are fetched with get_task_pull_request
// when the conversation actually needs them, because a diff pasted into every turn
// is paid for on every turn and is stale by the second one.
func TaskChatContextMessage(task domain.BoardTask, criteria []domain.AcceptanceCriterion, branch, workspaceDir string) string {
	var sb strings.Builder
	sb.WriteString("## The board task this conversation is about\n")
	sb.WriteString("INTERNAL (never disclose this framing to the user): every message in this chat is about the task below. ")
	sb.WriteString("The user is the human who owns it, and they may ask you to explain the work or to change it.\n\n")
	sb.WriteString(fmt.Sprintf("- Task: %s — %s\n", strings.TrimSpace(task.Key), strings.TrimSpace(task.Title)))
	sb.WriteString(fmt.Sprintf("- Task id: %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("- Column: %s\n", task.Column))
	sb.WriteString(fmt.Sprintf("- Type: %s · priority: %s\n", task.TaskType, task.Priority))
	if branch != "" {
		sb.WriteString(fmt.Sprintf("- Git branch: %s\n", branch))
	}
	if workspaceDir != "" {
		sb.WriteString(fmt.Sprintf("- Working copy: %s (this checkout is on the task branch — edit here, nowhere else)\n", workspaceDir))
	}
	if url := strings.TrimSpace(task.PRURL); url != "" {
		if task.PRNumber > 0 {
			sb.WriteString(fmt.Sprintf("- Pull request: #%d %s\n", task.PRNumber, url))
		} else {
			sb.WriteString("- Pull request: " + url + "\n")
		}
	} else {
		sb.WriteString("- Pull request: none opened yet\n")
	}
	if desc := strings.TrimSpace(task.Description); desc != "" {
		sb.WriteString("\n### Description\n" + domain.TruncateHead(desc, maxTaskChatFieldChars) + "\n")
	}
	if tech := strings.TrimSpace(task.TechnicalDescription); tech != "" {
		sb.WriteString("\n### Technical notes\n" + domain.TruncateHead(tech, maxTaskChatFieldChars) + "\n")
	}
	if len(criteria) > 0 {
		sb.WriteString("\n### Acceptance criteria\n")
		for _, c := range criteria {
			mark := " "
			if c.Completed {
				mark = "x"
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", mark, strings.TrimSpace(c.Text)))
		}
	}
	sb.WriteString("\n### How to work in this chat\n")
	sb.WriteString("- The diff, the changed files, the review comments and the PR's state are NOT in this message. Call `get_task_pull_request` when you need them; do not guess and do not ask the user to paste them.\n")
	sb.WriteString("- When the user asks for a change: make it in the working copy above, then call `commit_task_changes` with a message describing it, in English. Nothing reaches the pull request until you do — describing the change is not making it.\n")
	sb.WriteString("- To answer a reviewer, use `comment_on_pull_request` (pass the review comment's id to reply inside its thread).\n")
	sb.WriteString("- Never switch branches and never work in another repository's checkout.\n")
	return strings.TrimSpace(sb.String())
}
