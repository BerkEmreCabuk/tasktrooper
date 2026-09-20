package antigravity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

const mcpConfigRelPath = ".agents/mcp_config.json"

const mcpServerName = "tasktrooper"

type MCPConfig = core.MCPConfig

type MCPRun = core.MCPRun

type MCPProvider = core.MCPProvider

// writeMCPConfigFile lays the run's MCP server into the workspace's
// .agents/mcp_config.json for the run only.
func writeMCPConfigFile(workDir string, cfg core.MCPConfig) (func(), error) {
	noop := func() {}
	if !cfg.Set() {
		return noop, nil
	}
	headers := map[string]string{}
	if cfg.Token != "" {
		headers["Authorization"] = "Bearer " + cfg.Token
	}
	body, err := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{
			mcpServerName: map[string]any{
				"serverUrl": cfg.URL,
				"headers":   headers,
			},
		},
	}, "", "  ")
	if err != nil {
		return noop, fmt.Errorf("render antigravity mcp config: %w", err)
	}
	path := filepath.Join(workDir, mcpConfigRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, fmt.Errorf("create antigravity mcp config dir: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return noop, fmt.Errorf("write antigravity mcp config: %w", err)
	}
	cleanup := func() { _ = os.Remove(path) }
	return cleanup, nil
}