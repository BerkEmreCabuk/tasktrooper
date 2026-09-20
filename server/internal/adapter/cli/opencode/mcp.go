package opencode

import (
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

const mcpServerName = "tasktrooper"

type MCPConfig = core.MCPConfig

type MCPRun = core.MCPRun

type MCPProvider = core.MCPProvider

// mcpConfigContentEnv renders the run's MCP server as opencode's inline config
// env var. Nothing is written into the workspace, so there is no cleanup.
func mcpConfigContentEnv(cfg core.MCPConfig) (string, error) {
	if !cfg.Set() {
		return "", nil
	}
	headers := map[string]string{}
	if cfg.Token != "" {
		headers["Authorization"] = "Bearer " + cfg.Token
	}
	body, err := json.Marshal(map[string]any{
		"mcp": map[string]any{
			mcpServerName: map[string]any{
				"type":    "remote",
				"url":     cfg.URL,
				"headers": headers,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("render opencode mcp config: %w", err)
	}
	return string(body), nil
}