package projectmodel

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type toolsNoteInput struct {
	ShowGetProjectBrief     bool
	ShowListComponentChecks bool
	ShowListLinks           bool
	ShowGetEnvironment      bool
	ShowQueryRuntimeLogs    bool
	ShowListRuntimeErrors   bool
}

var toolsNoteKey = prompt.Define("projectmodel.tools_note", toolsNoteInput{ShowGetEnvironment: true})

// ToolsNote replaces injecting Brief's markdown into an agent's context: it
// only names the project-model tools the policy allows, so the agent fetches
// facts on demand instead of holding a copy that goes stale the moment the
// repository changes. It returns "" when the policy allows none of them.
func ToolsNote(policy domain.ToolPolicy) string {
	in := toolsNoteInput{
		ShowGetProjectBrief:     domain.ToolAllowedByPolicy("get_project_brief", policy),
		ShowListComponentChecks: domain.ToolAllowedByPolicy("list_component_checks", policy),
		ShowListLinks:           domain.ToolAllowedByPolicy("list_links", policy),
		ShowGetEnvironment:      domain.ToolAllowedByPolicy("get_environment", policy),
		ShowQueryRuntimeLogs:    domain.ToolAllowedByPolicy("query_runtime_logs", policy),
		ShowListRuntimeErrors:   domain.ToolAllowedByPolicy("list_runtime_errors", policy),
	}
	return strings.TrimRight(toolsNoteKey.Render(in), "\n")
}
