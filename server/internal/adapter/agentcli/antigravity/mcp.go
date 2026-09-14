package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mcpConfigRelPath is where AGY discovers a workspace's MCP servers
// (https://antigravity.google/docs/cli/mcp/: per-workspace config at
// .agents/mcp_config.json, read from the CLI's working directory). Unlike
// claudecode there is no per-invocation --mcp-config flag to point at a file
// elsewhere — spawn already sets cmd.Dir to the task workspace, so this is the
// one path AGY will ever look at for this run.
const mcpConfigRelPath = ".agents/mcp_config.json"

// mcpServerName is what the tools appear as inside the session. Kept the same
// as claudecode's for consistency, though the two configs are read by
// different CLIs and never merge.
const mcpServerName = "tasktrooper"

// MCPConfig points an AGY session at TaskTrooper's own tool surface, served by
// internal/adapter/mcpserver. With no MCPConfig set the session runs on AGY's
// native tools only.
type MCPConfig struct {
	// URL is the MCP endpoint (loopback for a session this process spawned,
	// the gateway-fronted address for a session on a member's Mac).
	URL string
	// Token is sent as a bearer credential — a per-run secret written to disk
	// only for the lifetime of one subprocess; see writeMCPConfigFile.
	Token string
	// Tools are the names the endpoint will serve this run. Unused by AGY's
	// config format today (which carries no manifest), kept for parity with
	// claudecode.MCPConfig and for a future prompt-side tool manifest.
	Tools []string
}

func (c MCPConfig) set() bool { return c.URL != "" }

// MCPRun is everything the endpoint needs to know about the caller it is
// minting a credential for. See claudecode.MCPRun for the fuller reasoning;
// this is the same shape, trimmed to what this executor actually threads
// through today.
type MCPRun struct {
	Policy domain.ToolPolicy
	Label  string
}

// MCPProvider mints ONE run's endpoint and credential. Implemented in
// platform/runtime, which is the only place that knows both the address this
// server bound and the token registry.
type MCPProvider interface {
	// ForRun mints the run's endpoint. The returned release is ALWAYS
	// non-nil, including on error, so the caller can defer it unconditionally.
	ForRun(ctx context.Context, run MCPRun) (MCPConfig, func(), error)
}

func (e *Executor) resolveMCP(ctx context.Context, run MCPRun) (MCPConfig, func(), error) {
	return resolveMCP(ctx, e.mcpProvider, e.mcp, run)
}

// resolveMCP is a free function so a static-config executor and a
// provider-backed one answer this question identically.
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

// writeMCPConfigFile renders AGY's workspace-level MCP config for one run and
// returns a cleanup that removes it. A no-op config renders nothing and
// returns a no-op cleanup, which is what a host with no MCP endpoint gets.
//
// The file is written INSIDE the workspace — unlike claudecode's
// --mcp-config file, kept in the OS temp dir — because AGY has no per-run flag
// to point elsewhere, only the fixed path it reads on its own. The bearer
// token this carries is therefore on disk only for the lifetime of one
// subprocess: the cleanup is deferred right after this call in Execute, so it
// runs before the board runner's own `git add -A` commit step ever looks at
// the workspace.
func writeMCPConfigFile(workDir string, cfg MCPConfig) (func(), error) {
	noop := func() {}
	if !cfg.set() {
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
