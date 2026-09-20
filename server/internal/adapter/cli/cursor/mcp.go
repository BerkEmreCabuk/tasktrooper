package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

const mcpConfigRelPath = ".cursor/mcp.json"

const mcpServerName = "tasktrooper"

type MCPConfig = core.MCPConfig

type MCPRun = core.MCPRun

type MCPProvider = core.MCPProvider

// writeMCPConfigFile merges the run's MCP server into the developer's
// .cursor/mcp.json for the run only, restoring the original afterwards.
func writeMCPConfigFile(workDir string, cfg core.MCPConfig) (func(), error) {
	noop := func() {}
	if !cfg.Set() {
		return noop, nil
	}
	path := filepath.Join(workDir, mcpConfigRelPath)
	original, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return noop, fmt.Errorf("read cursor mcp config: %w", readErr)
	}

	doc := map[string]any{}
	if existed {
		if err := json.Unmarshal(original, &doc); err != nil {
			return noop, fmt.Errorf("cursor mcp config at %s is not valid JSON, refusing to touch it: %w", path, err)
		}
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	headers := map[string]string{}
	if cfg.Token != "" {
		headers["Authorization"] = "Bearer " + cfg.Token
	}
	servers[mcpServerName] = map[string]any{
		"url":     cfg.URL,
		"headers": headers,
	}
	doc["mcpServers"] = servers

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return noop, fmt.Errorf("render cursor mcp config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, fmt.Errorf("create cursor mcp config dir: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return noop, fmt.Errorf("write cursor mcp config: %w", err)
	}

	cleanup := func() {
		if !existed {
			_ = os.Remove(path)
			return
		}
		_ = os.WriteFile(path, original, 0o600)
	}
	return cleanup, nil
}