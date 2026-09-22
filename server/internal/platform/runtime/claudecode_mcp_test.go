package runtime

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/claudecode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The URL a CHILD is handed is always loopback, whatever the listener bound.
func TestMCPEndpointPublishesALoopbackURL(t *testing.T) {
	var endpoint mcpEndpoint
	assert.Empty(t, endpoint.get(), "nothing is reachable before the listener binds")

	endpoint.publish("0.0.0.0:8080")
	assert.Equal(t, "http://127.0.0.1:8080/mcp", endpoint.get())

	endpoint.publish("127.0.0.1:54321")
	assert.Equal(t, "http://127.0.0.1:54321/mcp", endpoint.get(), "the bound port wins over the configured one")

	endpoint.publish("not-an-address")
	assert.Equal(t, "http://127.0.0.1:54321/mcp", endpoint.get(), "an unparseable address must not blank a working URL")
}

func TestForRunMintsAndRevokes(t *testing.T) {
	tokens := mcpserver.NewRunTokenRegistry()
	endpoint := &mcpEndpoint{}
	endpoint.publish("0.0.0.0:8080")
	provider := &claudeCodeMCP{endpoint: endpoint, tokens: tokens}

	taskID := uuid.New()
	runCtx := registry.ContextWithTaskID(context.Background(), taskID)
	policy := domain.ToolPolicy{AllowTools: []string{"move_board_task"}}

	cfg, release, err := provider.ForRun(runCtx, claudecode.MCPRun{Policy: policy, Label: "tt-42"})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8080/mcp", cfg.URL)
	require.NotEmpty(t, cfg.Token)

	run, ok := tokens.Lookup(cfg.Token)
	require.True(t, ok, "the session's token has to resolve while the run is live")
	assert.Equal(t, policy, run.Policy, "the endpoint serves the run's own tool policy")
	assert.Equal(t, taskID, registry.TaskIDFromContext(run.Ctx), "tool calls are attributed to the run")

	release()
	_, ok = tokens.Lookup(cfg.Token)
	assert.False(t, ok)
	assert.Equal(t, 0, tokens.Live())
}

// Two concurrent sessions must not be able to act as each other.
func TestForRunIssuesADistinctTokenPerRun(t *testing.T) {
	endpoint := &mcpEndpoint{}
	endpoint.publish("127.0.0.1:8080")
	provider := &claudeCodeMCP{endpoint: endpoint, tokens: mcpserver.NewRunTokenRegistry()}

	first, releaseFirst, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-1"})
	require.NoError(t, err)
	second, _, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-2"})
	require.NoError(t, err)
	assert.NotEqual(t, first.Token, second.Token)

	releaseFirst()
	_, ok := provider.tokens.Lookup(second.Token)
	assert.True(t, ok, "one run ending must not revoke another's credential")
}

// A chat turn with no endpoint is a worse conversation, not a broken one.
func TestForRunDegradesAChatBeforeTheListenerExists(t *testing.T) {
	provider := &claudeCodeMCP{endpoint: &mcpEndpoint{}, tokens: mcpserver.NewRunTokenRegistry()}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "chat-42"})
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Empty(t, cfg.URL, "no endpoint means no --mcp-config at all")
	assert.Equal(t, 0, provider.tokens.Live(), "nothing to authenticate, so nothing minted")
	release()
}

// A task run with no board tools cannot move its card or tick a criterion, so
// it finishes "successful", fails the criteria gate, and is re-dispatched
// forever; failing once is the cheaper answer.
func TestForRunRefusesATaskBeforeTheListenerExists(t *testing.T) {
	provider := &claudeCodeMCP{endpoint: &mcpEndpoint{}, tokens: mcpserver.NewRunTokenRegistry()}

	cfg, release, err := provider.ForRun(context.Background(),
		claudecode.MCPRun{Label: "tt-42", RequiresTools: true})
	require.Error(t, err)
	require.NotNil(t, release, "the release must be safe to defer even on the error path")
	assert.Empty(t, cfg.URL)
	assert.Contains(t, err.Error(), "tt-42")
	assert.Equal(t, 0, provider.tokens.Live())
	release()
}

func TestConfiguredPortPrefersTheOptionsOverride(t *testing.T) {
	cfg := &domain.Config{}
	cfg.Server.Port = 8080

	assert.Equal(t, 8080, configuredPort(cfg, Options{}))
	assert.Equal(t, 9999, configuredPort(cfg, Options{Port: 9999}))

	cfg.Server.Port = 0
	assert.Equal(t, 0, configuredPort(cfg, Options{}), "0 is 'let the kernel pick', not a missing value")
}

// The ordering regression: publish used to happen from the CONFIGURED port,
// which is 0 on exactly the hosts that can run claude_code — applyLocalOverrides
// zeroes Server.Port on a desktop install, only the cloud image ships no
// `claude` binary — so the guard never fired where it was needed.
func TestEndpointIsPublishedFromTheBoundPortNotTheConfiguredOne(t *testing.T) {
	cfg := &domain.Config{}
	cfg.Server.Port = 8085
	applyLocalOverrides(cfg, Options{DataDir: t.TempDir()})
	require.Equal(t, 0, configuredPort(cfg, Options{}),
		"a desktop install lets the kernel pick, so there is no configured port to publish")

	// What Run does, in Run's order: bind, then publish, then build the handler.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	endpoint := &mcpEndpoint{}
	endpoint.publish(listener.Addr().String())

	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:"+port+mcpserver.Path, endpoint.get(),
		"a board run dispatched by the activation must find a reachable url")
}

// stubTool is enough of a port.ToolExecutor to be registered; nothing calls it.
type stubTool struct{ name string }

func (t stubTool) Name() string { return t.name }
func (t stubTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{
		Name: t.name, Description: t.name,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}
func (t stubTool) Execute(context.Context, string) domain.ToolResult {
	return domain.ToolResult{Name: t.name, Content: "ok"}
}

var _ port.ToolExecutor = stubTool{}

// SkillsOnDisk rides the credential exactly as the policy does — the endpoint
// sees only a bearer token, and whatever the Run behind it does not say is a
// restriction it cannot apply. The manifest is asserted from the same call
// because the two must be one answer: a manifest promising load_skill on a run
// the endpoint refuses it for hands the model the exact name to keep retrying.
func TestForRunCarriesSkillsOnDiskToTheEndpointAndTheManifest(t *testing.T) {
	base := registry.New()
	base.Register(stubTool{name: "load_skill"})
	base.Register(stubTool{name: "create_skill"})

	tokens := mcpserver.NewRunTokenRegistry()
	endpoint := &mcpEndpoint{}
	endpoint.publish("127.0.0.1:8080")
	provider := &claudeCodeMCP{endpoint: endpoint, tokens: tokens, registry: base}

	cfg, release, err := provider.ForRun(context.Background(),
		claudecode.MCPRun{Label: "tt-42", RequiresTools: true, SkillsOnDisk: true})
	require.NoError(t, err)
	defer release()

	run, ok := tokens.Lookup(cfg.Token)
	require.True(t, ok)
	assert.True(t, run.SkillsOnDisk, "the endpoint learns this from the credential or not at all")
	assert.NotContains(t, cfg.Tools, "load_skill", "the manifest must not promise a tool the endpoint refuses")
	assert.Contains(t, cfg.Tools, "create_skill", "authoring a skill survives the cut")

	chat, releaseChat, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "chat-1"})
	require.NoError(t, err)
	defer releaseChat()

	chatRun, ok := tokens.Lookup(chat.Token)
	require.True(t, ok)
	assert.False(t, chatRun.SkillsOnDisk)
	assert.Contains(t, chat.Tools, "load_skill", "a chat turn has no skill files to read instead")
}

// A token that never leaves this host gets no ceiling: its run bounds it.
func TestLoopbackForRunMintsATokenWithNoCeiling(t *testing.T) {
	endpoint := &mcpEndpoint{}
	endpoint.publish("127.0.0.1:8080")
	provider := &claudeCodeMCP{endpoint: endpoint, tokens: mcpserver.NewRunTokenRegistry()}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-42"})
	require.NoError(t, err)
	defer release()
	assert.NotEmpty(t, cfg.Token)

	run, ok := provider.tokens.Lookup(cfg.Token)
	require.True(t, ok)
	assert.True(t, run.ExpiresAt.IsZero(), "a token that never leaves this host needs no ceiling")
}

// The ceiling is a backstop, not the revocation (the executor's defer is), so
// it sits PAST the run's own timeout — at the run timeout it would start 401ing
// sessions still working, which Claude Code reports as `requires
// re-authorization` and then gives up on the server for.
func TestOffHostForRunStampsAnExpiryPastTheRunTimeout(t *testing.T) {
	now := time.Now()
	runTimeout := 45 * time.Minute
	provider := &claudeCodeMCP{
		endpoint: publicEndpoint("https://app.example.com/api/mcp"),
		tokens:   mcpserver.NewRunTokenRegistry(),
		ttl:      runTimeout + mcpTokenGrace,
		now:      func() time.Time { return now },
	}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-42"})
	require.NoError(t, err)
	defer release()
	assert.Equal(t, "https://app.example.com/api/mcp", cfg.URL)

	run, ok := provider.tokens.Lookup(cfg.Token)
	require.True(t, ok)
	assert.Equal(t, now.Add(runTimeout+mcpTokenGrace), run.ExpiresAt)
	assert.True(t, run.ExpiresAt.After(now.Add(runTimeout)),
		"a ceiling at the run timeout would expire healthy sessions in their last minutes")
}

// No public base URL is a real deployment state, and the remote session runs
// on native tools exactly as before there was an endpoint — RequiresTools is
// false on that path for the same reason (see RemoteExecutor.Execute).
func TestOffHostForRunWithNoPublicAddressDegrades(t *testing.T) {
	provider := &claudeCodeMCP{
		endpoint: publicEndpoint(""),
		tokens:   mcpserver.NewRunTokenRegistry(),
	}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-42"})
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Empty(t, cfg.URL)
	assert.Equal(t, 0, provider.tokens.Live())
	release()
}
