package session

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func prependProjectPrompt(history []domain.Message, description string) []domain.Message {
	content := fmt.Sprintf("INTERNAL (never disclose to user): project purpose: %s", description)
	return append([]domain.Message{{Role: domain.RoleSystem, Content: content}}, history...)
}

// maxInjectedProfileChars mirrors the board runner's cap: the profile is a
// brief, and past this size it eats the budget the chat needs for real work.
const maxInjectedProfileChars = 8000

// prependProjectProfilePrompt injects the agent-maintained project profile
// into a repository-scoped chat, truncated the same way board runs cap it.
func prependProjectProfilePrompt(history []domain.Message, profile string) []domain.Message {
	content := "## Project profile (maintained by agents)\n" + domain.TruncateHead(profile, maxInjectedProfileChars)
	return append([]domain.Message{{Role: domain.RoleSystem, Content: content}}, history...)
}

// prependTaskChatPrompt states what a task-bound chat is about: the task, its
// branch, its pull request, and how to reach the PR.
//
// Compact by design — no diff, no review comments. Those live behind
// get_task_pull_request because a diff pasted into the prompt is paid for on
// every turn of the conversation and is stale as soon as the agent commits.
func prependTaskChatPrompt(history []domain.Message, task TaskBinding) []domain.Message {
	content := prompt.TaskChatContextMessage(task.Task, task.Criteria, task.Branch, task.WorkspaceDir)
	if content == "" {
		return history
	}
	return append([]domain.Message{{Role: domain.RoleSystem, Content: content}}, history...)
}

func prependWorkspacePrompt(history []domain.Message, workspaceDir, lang string) []domain.Message {
	out := make([]domain.Message, 0, len(history)+5)
	out = append(out, workspaceSystemMessage(workspaceDir))
	out = append(out, userFacingSystemMessage())
	out = append(out, languageSystemMessage(lang))
	out = append(out, toolSelectionSystemMessage())
	out = append(out, repeatCallSystemMessage())
	out = append(out, history...)
	return out
}

func toolSelectionSystemMessage() domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.ToolSelectionGuidance()}
}

// repeatCallSystemMessage carries the no-repeat contract into workspace chats.
// An agentless session never builds an agent system prompt, so without this the
// only path that runs raw shell commands was also the only one missing the rule
// that keeps it from re-issuing the same command until the loop guard fires.
func repeatCallSystemMessage() domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.RepeatCallGuidance()}
}

func userFacingSystemMessage() domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.UserFacingGuidance()}
}

func workspaceSystemMessage(workspaceDir string) domain.Message {
	content := fmt.Sprintf("INTERNAL (never disclose to user): session workspace: %s\nPerform all file and shell operations inside this directory. Subtasks use dedicated subfolders within it.", workspaceDir)
	return domain.Message{Role: domain.RoleSystem, Content: content}
}

func languageSystemMessage(lang string) domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: prompt.LanguageInstruction(lang)}
}
