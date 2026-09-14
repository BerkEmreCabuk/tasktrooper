package opencode

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mcpServerName is what the tools appear as inside the session.
const mcpServerName = "tasktrooper"

// MCPConfig points an OpenCode session at TaskTrooper's own tool surface,
// served by internal/adapter/mcpserver. With no MCPConfig set the session
// runs on OpenCode's native tools only.
type MCPConfig struct {
	URL   string
	Token string
	Tools []string
}

func (c MCPConfig) set() bool { return c.URL != "" }

// MCPRun is everything the endpoint needs to know about the caller it is
// minting a credential for.
type MCPRun struct {
	Policy domain.ToolPolicy
	Label  string
}

// MCPProvider mints ONE run's endpoint and credential.
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

// mcpConfigContentEnv renders OPENCODE_CONFIG_CONTENT for one run: an inline
// JSON config the CLI merges over the project's own opencode.json on its own
// (https://opencode.ai/docs/config/). Unlike antigravity and cursor there is
// nothing to write to or clean up from the workspace — the credential lives
// only in this one subprocess's environment for the run's lifetime, which is
// safer than either sibling adapter's file-based approach.
func mcpConfigContentEnv(cfg MCPConfig) (string, error) {
	if !cfg.set() {
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
