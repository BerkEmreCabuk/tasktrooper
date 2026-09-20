package core

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// MCPConfig is one run's TaskTrooper MCP server materialization: the URL the
// CLI is pointed at and the per-run bearer token minted for it.
type MCPConfig struct {
	URL   string
	Token string
	Tools []string
}

func (c MCPConfig) Set() bool { return c.URL != "" }

// MCPRun carries the facts a provider needs to mint a per-run MCP session.
type MCPRun struct {
	Policy        domain.ToolPolicy
	Label         string
	RequiresTools bool
	SkillsOnDisk  bool
}

// MCPProvider mints the run-scoped MCP config and a release function that
// revokes the run's token.
type MCPProvider interface {
	ForRun(ctx context.Context, run MCPRun) (MCPConfig, func(), error)
}

func ResolveMCP(ctx context.Context, provider MCPProvider, static MCPConfig, run MCPRun) (MCPConfig, func(), error) {
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

// ToolManifest renders the run's tool list into prose the system prompt can
// carry, so the model learns the mcp__tasktrooper__ prefix without the CLI
// having to hand it over.
func ToolManifest(serverName string, names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := "mcp__" + serverName + "__"
	prefixed := make([]string, 0, len(names))
	for _, name := range names {
		prefixed = append(prefixed, prefix+name)
	}
	return "TaskTrooper's own tools reach you through the `" + serverName + "` MCP server, so their real names carry the `" +
		prefix + "` prefix: the tool this system's instructions call `set_criterion_completed` is called as `" +
		prefix + "set_criterion_completed`. They are already available to you — do NOT search for them and do not report one as missing " +
		"because an unprefixed name did not resolve. These are the ones this run has, in full:\n" +
		strings.Join(prefixed, ", ") + "."
}

func WithToolManifest(systemPrompt, serverName string, names []string) string {
	manifest := ToolManifest(serverName, names)
	if manifest == "" || strings.TrimSpace(systemPrompt) == "" {
		return systemPrompt
	}
	return systemPrompt + "\n\n" + manifest
}

// MCPConfigJSON renders the run's MCP config in the shape the CLIs read from
// their --mcp-config/…-config file: one HTTP server carrying the run token.
func MCPConfigJSON(serverName string, cfg MCPConfig) ([]byte, error) {
	headers := map[string]string{}
	if cfg.Token != "" {
		headers["Authorization"] = "Bearer " + cfg.Token
	}
	return json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			serverName: map[string]any{
				"type":    "http",
				"url":     cfg.URL,
				"headers": headers,
			},
		},
	})
}