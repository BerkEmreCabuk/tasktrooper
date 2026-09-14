package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mcpConfigRelPath is Cursor's own project-level MCP config path — the same
// file the Cursor IDE reads, which is exactly the risk: a repository is
// likely to already have this file committed with the developer's own
// servers in it.
const mcpConfigRelPath = ".cursor/mcp.json"

const mcpServerName = "tasktrooper"

// MCPConfig points a cursor-agent session at TaskTrooper's own tool surface.
type MCPConfig struct {
	URL   string
	Token string
	Tools []string
}

func (c MCPConfig) set() bool { return c.URL != "" }

type MCPRun struct {
	Policy domain.ToolPolicy
	Label  string
}

type MCPProvider interface {
	ForRun(ctx context.Context, run MCPRun) (MCPConfig, func(), error)
}

func (e *Executor) resolveMCP(ctx context.Context, run MCPRun) (MCPConfig, func(), error) {
	return resolveMCP(ctx, e.mcpProvider, e.mcp, run)
}

func resolveMCP(ctx context.Context, provider MCPProvider, static MCPConfig, run MCPRun) (MCPConfig, func(), error) {
	noop := func() {}
	if provider == nil {
		return static, noop, nil
	}
	cfg, release, err := provider.ForRun(ctx, run)
	if release == nil {
		release = noop
	}
	if err != nil {
		return MCPConfig{}, release, err
	}
	return cfg, release, nil
}

// writeMCPConfigFile upserts the tasktrooper server into .cursor/mcp.json for
// one run and returns a cleanup that restores the file to exactly what it was
// before.
//
// A merge, not an overwrite: .cursor/mcp.json is a real Cursor IDE file a
// repository is likely to already have committed, with the developer's own
// servers in it. Overwriting it would work for one run and destroy their
// config for every session after. The restore is a byte-for-byte rewrite of
// the original content when the file already existed, or a delete when this
// call is what created it — so a run that never touched this file leaves no
// trace, and a run that did leaves the file exactly as it found it.
//
// A malformed pre-existing file is left untouched and the run fails loudly
// rather than silently discarding whatever the developer had there.
func writeMCPConfigFile(workDir string, cfg MCPConfig) (func(), error) {
	noop := func() {}
	if !cfg.set() {
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
