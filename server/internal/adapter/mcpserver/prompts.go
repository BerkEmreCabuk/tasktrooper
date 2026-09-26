package mcpserver

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers the refusal text (*Server).unavailable returns to an
// agent cli session — see catalog/system/prompts/mcp/**.

type toolNameInput struct{ Name string }

var (
	askUserUnavailableKey   = prompt.Define("mcp.ask_user_unavailable", toolNameInput{Name: "ask_user"})
	skillLoadUnavailableKey = prompt.Define("mcp.skill_load_unavailable", toolNameInput{Name: skillLoadTool})
	toolNotExposedKey       = prompt.Define("mcp.tool_not_exposed", toolNameInput{Name: "read_file"})
	toolNotRegisteredKey    = prompt.Define("mcp.tool_not_registered", toolNameInput{Name: "set_criterion_completed"})
	toolPolicyDeniedKey     = prompt.Define("mcp.tool_policy_denied", toolNameInput{Name: "mcp_github_create_pr"})
)
