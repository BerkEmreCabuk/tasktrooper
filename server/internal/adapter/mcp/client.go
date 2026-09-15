package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const namespaceSep = "_"

type mcpToolExecutor struct {
	toolName   string
	serverID   string
	rawName    string
	session    *sdkmcp.ClientSession
	definition domain.ToolDefinition
}

func (e *mcpToolExecutor) Name() string {
	return e.toolName
}

func (e *mcpToolExecutor) Definition() domain.ToolDefinition {
	return e.definition
}

func (e *mcpToolExecutor) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args any
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return domain.ToolResult{
				Name:    e.toolName,
				Content: fmt.Sprintf("invalid arguments: %v", err),
				IsError: true,
			}
		}
	}

	result, err := e.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      e.rawName,
		Arguments: args,
	})
	if err != nil {
		return domain.ToolResult{
			Name:    e.toolName,
			Content: fmt.Sprintf("mcp call error: %v", err),
			IsError: true,
		}
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			parts = append(parts, tc.Text)
		} else {
			raw, _ := json.Marshal(c)
			parts = append(parts, string(raw))
		}
	}

	return domain.ToolResult{
		Name:    e.toolName,
		Content: strings.Join(parts, "\n"),
		IsError: result.IsError,
	}
}

func NamespacedToolName(serverID, toolName string) string {
	return fmt.Sprintf("mcp%s%s%s%s", namespaceSep, serverID, namespaceSep, toolName)
}

func connectServer(ctx context.Context, cfg domain.MCPServerConfig, policy urlguard.Policy) ([]*mcpToolExecutor, func(), error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "local-llm-bridge",
		Version: "1.0.0",
	}, nil)

	var transport sdkmcp.Transport
	switch cfg.Transport {
	case "stdio":
		if cfg.Command == "" {
			return nil, nil, fmt.Errorf("stdio transport requires command")
		}
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = stdioServerEnv(cfg.Env)
		transport = &sdkmcp.CommandTransport{Command: cmd}
	case "http":
		if cfg.URL == "" {
			return nil, nil, fmt.Errorf("http transport requires url")
		}
		httpClient, err := guardedMCPClient(ctx, cfg, policy)
		if err != nil {
			return nil, nil, err
		}
		transport = &sdkmcp.StreamableClientTransport{
			Endpoint:             cfg.URL,
			HTTPClient:           httpClient,
			DisableStandaloneSSE: true,
		}
	default:
		return nil, nil, fmt.Errorf("unsupported transport: %q (use 'stdio' or 'http')", cfg.Transport)
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}

	closeFunc := func() {
		if err := session.Close(); err != nil {
			log.Warn().Err(err).Str("server", cfg.ID).Msg("mcp session close error")
		}
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		closeFunc()
		return nil, nil, fmt.Errorf("list tools: %w", err)
	}

	allowedSet := make(map[string]bool)
	for _, t := range cfg.AllowedTools {
		allowedSet[t] = true
	}

	var executors []*mcpToolExecutor
	for _, tool := range result.Tools {
		if len(allowedSet) > 0 && !allowedSet[tool.Name] {
			continue
		}

		nsName := NamespacedToolName(cfg.ID, tool.Name)

		params, err := toolInputSchema(tool.InputSchema)
		if err != nil {
			log.Warn().Err(err).Str("tool", tool.Name).Msg("could not parse input schema, using empty schema")
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}

		executor := &mcpToolExecutor{
			toolName: nsName,
			serverID: cfg.ID,
			rawName:  tool.Name,
			session:  session,
			definition: domain.ToolDefinition{
				Type: "function",
				Function: domain.FunctionDefinition{
					Name:        nsName,
					Description: tool.Description,
					Parameters:  params,
				},
			},
		}
		executors = append(executors, executor)
		log.Debug().Str("server", cfg.ID).Str("tool", nsName).Msg("discovered mcp tool")
	}

	return executors, closeFunc, nil
}

// stdioServerEnv builds the environment for a stdio MCP server subprocess.
//
// It used to be the whole process environment plus the config's overrides, so
// every MCP server the user could configure ran with the pod's DATABASE_URL,
// INTERNAL_AUTH_KEY and MCP_SECRETS_KEY in reach — an npx package away from
// posting them anywhere. An MCP server config is user input; it is
// attacker-influenced in exactly the way a run_terminal command is, and gets
// the same treatment.
//
// The distinction that matters here: the *ambient* environment is scrubbed, the
// *configured* one is not. An MCP server config normally supplies that server's
// own credentials (a GitHub token, a vendor API key), and those are passed as
// the overlay so they still reach the subprocess — including a deliberate
// override of a forwarded variable such as PATH or HOME, since the overlay is
// appended last and os/exec keeps the last value for a repeated name.
//
// Overrides are sorted so a config that sets the same key twice (it cannot —
// it is a map — but a future caller might) and the resulting command line stay
// deterministic across runs, which keeps failures reproducible.
func stdioServerEnv(overrides map[string]string) []string {
	configured := make([]string, 0, len(overrides))
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		configured = append(configured, key+"="+overrides[key])
	}
	return childenv.For(os.Environ(), configured)
}

func toolInputSchema(schema any) (map[string]interface{}, error) {
	if schema == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// guardedMCPClient builds the HTTP client for an MCP endpoint.
//
// Two things were wrong with the plain &http.Client{Timeout: 30s} this replaces.
//
// First, the endpoint is agent-configured: the operator (or a prompt injection
// that reaches the MCP settings) picks the URL, and cfg.Headers is an arbitrary
// attacker-chosen map — which is what makes this the one outbound path that can
// satisfy GCP's Metadata-Flavor: Google gate. So the destination is resolved and
// checked here, at connect time, not only where the row was written: rows stored
// before the write-time check existed, or edited around it, arrive here anyway.
//
// Second, redirects. Go's http.Client strips Authorization and Cookie when a
// redirect changes host, but it strips them from the request it builds, and
// headerTransport sits below that and used to put them straight back. With no
// CheckRedirect set, the default ten hops applied. A compromised or lookalike
// MCP host answering 302 replayed the caller's bearer token to the attacker in
// cleartext. Cross-host redirects are now refused outright — the same defence
// adapter/appstore/client.go already applies to ASC's paging links, where the
// host always stays the configured one.
func guardedMCPClient(ctx context.Context, cfg domain.MCPServerConfig, policy urlguard.Policy) (*http.Client, error) {
	target, err := policy.Validate(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("mcp endpoint refused: %w", err)
	}

	client := policy.ClientFor(target, 30*time.Second)
	client.CheckRedirect = policy.SameHostRedirect(target.URL)
	if len(cfg.Headers) > 0 {
		client.Transport = &headerTransport{
			base:    client.Transport,
			headers: cfg.Headers,
			host:    target.URL.Host,
		}
	}
	return client, nil
}

// headerTransport applies the server's configured headers to every request.
//
// The headers are keyed to the configured host on purpose. Belt and braces with
// the CheckRedirect above: a RoundTripper cannot see that net/http decided to
// strip a credential for this hop, so the only thing it can do is refuse to
// re-attach one to a host that is not the one the credential belongs to.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
	host    string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.host != "" && !strings.EqualFold(req.URL.Host, t.host) {
		return nil, fmt.Errorf("mcp: refusing to send configured headers to %q, expected %q", req.URL.Host, t.host)
	}
	reqCopy := req.Clone(req.Context())
	for k, v := range t.headers {
		reqCopy.Header.Set(k, v)
	}
	return t.base.RoundTrip(reqCopy)
}

type serverHealth struct {
	ID        string
	Connected bool
	Disabled  bool
	ToolCount int
	Tools     []string
	LastError string
}

// Manager is read from request goroutines (Health, via /mcp and the tools
// endpoint) while MCP servers connect on a background goroutine, so both slices
// need the mutex — a torn slice-header read would panic inside a handler.
type Manager struct {
	mu         sync.RWMutex
	closeFuncs []func()
	servers    []serverHealth
	policy     urlguard.Policy
}

// SetURLPolicy overrides the policy HTTP MCP endpoints are vetted against.
// Unset (the zero value, which is how runtime builds a Manager) means
// urlguard.Default — public internet only, plus loopback when a self-hosted
// operator has set ALLOW_LOOPBACK_TOOL_URLS.
func (m *Manager) SetURLPolicy(p urlguard.Policy) {
	m.mu.Lock()
	m.policy = p
	m.mu.Unlock()
}

// urlPolicy resolves the policy lazily rather than in a constructor, because a
// Manager is legitimately created as &Manager{} and a zero Policy allows no
// scheme at all — failing closed is right for a forgotten field, but it would be
// wrong to let that shape reach a caller that never asked for a policy.
func (m *Manager) urlPolicy() urlguard.Policy {
	m.mu.RLock()
	p := m.policy
	m.mu.RUnlock()
	if len(p.Schemes) == 0 {
		return urlguard.Default()
	}
	return p
}

func (m *Manager) Health() []map[string]interface{} {
	m.mu.RLock()
	servers := append([]serverHealth(nil), m.servers...)
	m.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(servers))
	for _, s := range servers {
		entry := map[string]interface{}{
			"id":         s.ID,
			"connected":  s.Connected,
			"tool_count": s.ToolCount,
		}
		if s.Disabled {
			entry["status"] = "disabled"
			entry["connected"] = false
		} else if s.Connected {
			entry["status"] = "connected"
		} else {
			entry["status"] = "error"
		}
		if s.LastError != "" {
			entry["last_error"] = s.LastError
		}
		if len(s.Tools) > 0 {
			entry["tools"] = s.Tools
		}
		result = append(result, entry)
	}
	return result
}

func (m *Manager) LoadAndRegister(ctx context.Context, configs []domain.MCPServerConfig, registry port.ToolRegistry) {
	// Built locally and published under the lock at the end, so Health() never
	// observes a half-filled server list.
	var (
		servers    []serverHealth
		closeFuncs []func()
	)
	defer func() {
		m.mu.Lock()
		m.servers = servers
		m.closeFuncs = append(m.closeFuncs, closeFuncs...)
		m.mu.Unlock()
	}()
	policy := m.urlPolicy()
	for _, cfg := range configs {
		if !cfg.Enabled {
			servers = append(servers, serverHealth{ID: cfg.ID, Connected: false, Disabled: true})
			log.Debug().Str("server", cfg.ID).Msg("mcp server disabled, skipping")
			continue
		}

		connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		executors, closeFunc, err := connectServer(connectCtx, cfg, policy)
		cancel()
		if err != nil {
			log.Error().Err(err).Str("server", cfg.ID).Msg("failed to connect mcp server, skipping")
			servers = append(servers, serverHealth{ID: cfg.ID, Connected: false, LastError: err.Error()})
			continue
		}

		closeFuncs = append(closeFuncs, closeFunc)
		toolNames := make([]string, 0, len(executors))
		for _, e := range executors {
			registry.Register(e)
			toolNames = append(toolNames, e.Name())
		}
		servers = append(servers, serverHealth{ID: cfg.ID, Connected: true, ToolCount: len(executors), Tools: toolNames})
		log.Info().Str("server", cfg.ID).Int("tools", len(executors)).Msg("mcp server connected")
	}
}

func (m *Manager) Reload(ctx context.Context, configs []domain.MCPServerConfig, registry port.ToolRegistry) {
	m.Close()
	m.LoadAndRegister(ctx, configs, registry)
}

// Close shuts every connected server down and drops the close funcs, so a
// following Reload does not run them twice.
func (m *Manager) Close() {
	m.mu.Lock()
	closeFuncs := m.closeFuncs
	m.closeFuncs = nil
	m.mu.Unlock()
	for _, fn := range closeFuncs {
		fn()
	}
}
