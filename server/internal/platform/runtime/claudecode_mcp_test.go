package runtime

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/agentcli/claudecode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// The URL a CHILD is handed is always loopback, whatever the listener bound. A
// cloud pod listens on 0.0.0.0, which nothing can connect to.
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

// One run, one token: minted with the run's own context and policy so the
// endpoint can execute its calls as that run, and revoked the moment the run
// releases it.
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

// A CHAT turn that starts before the listener does still does real work on the
// CLI's native tools: fewer tools is a worse conversation, not a broken one.
func TestForRunDegradesAChatBeforeTheListenerExists(t *testing.T) {
	provider := &claudeCodeMCP{endpoint: &mcpEndpoint{}, tokens: mcpserver.NewRunTokenRegistry()}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "chat-42"})
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Empty(t, cfg.URL, "no endpoint means no --mcp-config at all")
	assert.Equal(t, 0, provider.tokens.Live(), "nothing to authenticate, so nothing minted")
	release()
}

// A TASK does not degrade. A board run with no TaskTrooper tools cannot move its
// card, tick an acceptance criterion or record a verdict — so it finishes
// looking successful, fails the criteria gate, and is dispatched again, forever,
// spending the subscription on every lap. Failing once is the cheaper answer.
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

// The ordering regression, stated as the two facts that used to contradict each
// other.
//
// buildHandler used to publish from the CONFIGURED port on the reasoning that
// the board workers it activates at its end must not start before the URL
// exists. That publish was guarded by `port > 0` — and the port is 0 on exactly
// the hosts that can run a claude_code agent: the cloud image ships no `claude`
// binary, so the executor only registers on a desktop or runner install, where
// applyDesktopOverrides sets Server.Port to 0. The guard therefore never fired
// where it was needed, and the endpoint was published only after buildHandler
// had already started the workers.
//
// Run now binds the listener BEFORE buildHandler and publishes the address the
// kernel actually gave it, so the URL is a fact rather than a configured guess.
func TestEndpointIsPublishedFromTheBoundPortNotTheConfiguredOne(t *testing.T) {
	cfg := &domain.Config{}
	cfg.Server.Port = 8085
	applyLocalOverrides(cfg, Options{DataDir: t.TempDir()})
	require.Equal(t, 0, configuredPort(cfg, Options{}),
		"a desktop install lets the kernel pick, so there is no configured port to publish")

	// What Run does, in Run's order: bind, then publish, then build the handler
	// (which is what starts the board workers).
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

// stubTool is enough of a port.ToolExecutor to be registered and to have a
// definition; nothing here calls it.
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

// SkillsOnDisk narrows the surface exactly as the policy does, so it has to
// travel with the credential in exactly the same way: the endpoint only ever
// sees a bearer token, and whatever the Run behind it does not say is a
// restriction the endpoint cannot apply.
//
// The manifest is asserted from the same call because the two must be one
// answer. The manifest is what the session's system prompt tells the model it
// holds, so a manifest naming load_skill on a run the endpoint will refuse it
// for produces the worst outcome available: a model that has been handed the
// exact tool name, retries it, and is refused every time.
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

	// A chat turn, whose workspace nothing materialised into.
	chat, releaseChat, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "chat-1"})
	require.NoError(t, err)
	defer releaseChat()

	chatRun, ok := tokens.Lookup(chat.Token)
	require.True(t, ok)
	assert.False(t, chatRun.SkillsOnDisk)
	assert.Contains(t, chat.Tools, "load_skill", "a chat turn has no skill files to read instead")
}

// The tenant is bound to the credential at mint time, because that is the only
// moment it is available: /mcp is a public path, so no signed tenant header
// reaches the endpoint and tenantMiddleware never runs for it.
func TestForRunBindsTheDispatchingTenantToTheToken(t *testing.T) {
	tenantID := uuid.New()
	tokens := mcpserver.NewRunTokenRegistry()
	endpoint := &mcpEndpoint{}
	endpoint.publish("127.0.0.1:8080")
	provider := &claudeCodeMCP{endpoint: endpoint, tokens: tokens}

	runCtx := tenant.With(context.Background(), tenant.Identity{
		TenantID: tenantID, Role: tenant.RoleMember, UserID: "uid-7",
	})
	cfg, release, err := provider.ForRun(runCtx, claudecode.MCPRun{Label: "tt-42"})
	require.NoError(t, err)
	defer release()

	run, ok := tokens.Lookup(cfg.Token)
	require.True(t, ok)
	assert.Equal(t, tenantID, run.Tenant.TenantID)

	id, ok := tenant.From(run.Scoped())
	require.True(t, ok, "every tool call this token makes must be tenant-scoped")
	assert.Equal(t, tenantID, id.TenantID)
	assert.Equal(t, "uid-7", id.UserID)
}

// A credential that leaves this machine must name a tenant. Refused at the
// mint rather than discovered later: a token published to somebody's laptop is
// not a thing to issue unscoped and fix afterwards.
func TestOffHostForRunRefusesARunWithNoTenant(t *testing.T) {
	provider := &claudeCodeMCP{
		endpoint: publicEndpoint("https://app.example.com/api/mcp"),
		tokens:   mcpserver.NewRunTokenRegistry(),
		offHost:  true,
	}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-42"})
	require.Error(t, err)
	require.NotNil(t, release, "the release must be safe to defer even on the error path")
	assert.Empty(t, cfg.Token)
	assert.Contains(t, err.Error(), "tt-42")
	assert.Equal(t, 0, provider.tokens.Live())
	release()
}

// The same run on the LOOPBACK path is not refused: a self-hosted or desktop
// install carries no identity on its run context and worked that way before
// tenants existed. Its failure mode, if the deployment is in fact multi-tenant,
// is the database's own fail-closed check on this same process — not a token
// published to a stranger's machine.
func TestLoopbackForRunAllowsASingleTenantInstall(t *testing.T) {
	endpoint := &mcpEndpoint{}
	endpoint.publish("127.0.0.1:8080")
	provider := &claudeCodeMCP{endpoint: endpoint, tokens: mcpserver.NewRunTokenRegistry()}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-42"})
	require.NoError(t, err)
	defer release()
	assert.NotEmpty(t, cfg.Token)

	run, ok := provider.tokens.Lookup(cfg.Token)
	require.True(t, ok)
	assert.Equal(t, uuid.Nil, run.Tenant.TenantID)
	assert.True(t, run.ExpiresAt.IsZero(), "a token that never leaves this host needs no ceiling")
}

// An off-host token expires. It is a backstop, not the revocation — the
// executor's defer is that — so it is set PAST the run's own timeout, or it
// would start 401ing sessions that are still working, which Claude Code reports
// as `requires re-authorization` and then gives up on the server for.
func TestOffHostForRunStampsAnExpiryPastTheRunTimeout(t *testing.T) {
	now := time.Now()
	runTimeout := 45 * time.Minute
	provider := &claudeCodeMCP{
		endpoint: publicEndpoint("https://app.example.com/api/mcp"),
		tokens:   mcpserver.NewRunTokenRegistry(),
		offHost:  true,
		ttl:      runTimeout + mcpTokenGrace,
		now:      func() time.Time { return now },
	}

	runCtx := tenant.With(context.Background(), tenant.Identity{TenantID: uuid.New()})
	cfg, release, err := provider.ForRun(runCtx, claudecode.MCPRun{Label: "tt-42"})
	require.NoError(t, err)
	defer release()
	assert.Equal(t, "https://app.example.com/api/mcp", cfg.URL)

	run, ok := provider.tokens.Lookup(cfg.Token)
	require.True(t, ok)
	assert.Equal(t, now.Add(runTimeout+mcpTokenGrace), run.ExpiresAt)
	assert.True(t, run.ExpiresAt.After(now.Add(runTimeout)),
		"a ceiling at the run timeout would expire healthy sessions in their last minutes")
}

// No public base URL is a real deployment state, not a failure: the remote
// session runs on the CLI's native tools, exactly as it did before there was an
// endpoint to offer it. RequiresTools is false on that path for the same
// reason — see RemoteExecutor.Execute.
func TestOffHostForRunWithNoPublicAddressDegrades(t *testing.T) {
	provider := &claudeCodeMCP{
		endpoint: publicEndpoint(""),
		tokens:   mcpserver.NewRunTokenRegistry(),
		offHost:  true,
	}

	cfg, release, err := provider.ForRun(context.Background(), claudecode.MCPRun{Label: "tt-42"})
	require.NoError(t, err)
	require.NotNil(t, release)
	assert.Empty(t, cfg.URL)
	assert.Equal(t, 0, provider.tokens.Live())
	release()
}
