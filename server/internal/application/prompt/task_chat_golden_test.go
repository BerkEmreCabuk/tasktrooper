package prompt_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func taskChatFixtures() map[string]struct {
	task     domain.BoardTask
	criteria []domain.AcceptanceCriterion
} {
	return map[string]struct {
		task     domain.BoardTask
		criteria []domain.AcceptanceCriterion
	}{
		"no_pr_no_desc": {
			task: domain.BoardTask{Key: "TT-1", Title: "Fix the bug", Column: "todo"},
		},
		"pr_with_number": {
			task: domain.BoardTask{
				Key: "TT-2", Title: "Add feature", Column: "in_progress",
				Description: "Add the widget to the dashboard.", PRURL: "https://github.com/o/r/pull/42", PRNumber: 42,
			},
			criteria: []domain.AcceptanceCriterion{
				{Text: "Widget renders", Completed: true},
				{Text: "Widget updates live", Completed: false},
			},
		},
		"pr_without_number": {
			task: domain.BoardTask{
				Key: "TT-3", Title: "Investigate flake", Column: "code_review",
				Description: "Tracks down the flaky test.", TechnicalDescription: "See CI logs from run 88.",
				PRURL: "https://github.com/o/r/pull/999",
			},
		},
	}
}

func TestGoldenTaskChatOpeningMessage(t *testing.T) {
	for name, fx := range taskChatFixtures() {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, "task_chat_opening_"+name, prompt.TaskChatOpeningMessage(fx.task, fx.criteria))
		})
	}
}

func TestGoldenTaskChatContextMessage(t *testing.T) {
	for name, fx := range taskChatFixtures() {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, "task_chat_context_"+name, prompt.TaskChatContextMessage(fx.task, fx.criteria, "task/tt-2", "/data/ws/tt-2"))
		})
	}
	assertGolden(t, "task_chat_context_no_branch_no_workspace",
		prompt.TaskChatContextMessage(domain.BoardTask{Key: "TT-9", Title: "Bare", Column: "todo"}, nil, "", ""))
}
