package core

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers ToolManifest's rendered text — see
// catalog/system/prompts/cli/tool_manifest.md.

type toolManifestInput struct {
	ServerName    string
	Prefix        string
	PrefixedNames []string
}

var toolManifestKey = prompt.Define("cli.tool_manifest", toolManifestInput{
	ServerName:    "tasktrooper",
	Prefix:        "mcp__tasktrooper__",
	PrefixedNames: []string{"mcp__tasktrooper__set_criterion_completed"},
})
