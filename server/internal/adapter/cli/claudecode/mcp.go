package claudecode

import (
	"fmt"
	"os"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

type MCPConfig = core.MCPConfig

type MCPRun = core.MCPRun

type MCPProvider = core.MCPProvider

const mcpServerName = "tasktrooper"

const toolNamePrefix = "mcp__" + mcpServerName + "__"

func toolManifest(names []string) string {
	return core.ToolManifest(mcpServerName, names)
}

func withToolManifest(systemPrompt string, names []string) string {
	return core.WithToolManifest(systemPrompt, mcpServerName, names)
}

func writeMCPConfigFile(cfg MCPConfig) (string, func(), error) {
	noop := func() {}
	if !cfg.Set() {
		return "", noop, nil
	}
	body, err := core.MCPConfigJSON(mcpServerName, cfg)
	if err != nil {
		return "", noop, fmt.Errorf("render mcp config: %w", err)
	}
	f, err := os.CreateTemp("", "tt-claude-mcp-*.json")
	if err != nil {
		return "", noop, fmt.Errorf("create mcp config file: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("secure mcp config file: %w", err)
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("write mcp config file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close mcp config file: %w", err)
	}
	return path, cleanup, nil
}