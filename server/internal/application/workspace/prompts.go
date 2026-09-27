package workspace

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers the LLM-facing string ValidateTransition renders —
// see catalog/system/guards/workspace_transition_not_allowed.md. It
// reaches an agent through repository.Service's move_board_task/update_task
// path, which calls ValidateTransition before a column change.

var transitionNotAllowedKey = prompt.Define[struct{}]("guard.workspace_transition_not_allowed", struct{}{})
