package agentfs

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers the "no system prompt, no rules" fallback role line
// both claude.go and cursor.go render — see
// catalog/system/prompts/agentfs/default_agent_role.md.

type agentNameInput struct{ Name string }

var defaultAgentRoleKey = prompt.Define("agentfs.default_agent_role", agentNameInput{Name: "Reviewer"})
