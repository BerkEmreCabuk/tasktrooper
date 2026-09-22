package runtime

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/claudecode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// mcpEndpoint publishes the loopback URL a CHILD process on this host POSTs to
// for this server's MCP endpoint: Run binds FIRST and publishes the bound
// address, because with Server.Port 0 the port is not even chosen until the
// listener binds, and a run can be executing before buildHandler returns. The
// host is always 127.0.0.1, never whatever the listener bound — a cloud pod
// listens on 0.0.0.0, which nothing can connect to, and the only client is a
// process on this same host.
type mcpEndpoint struct {
	url atomic.Pointer[string]
}

func (e *mcpEndpoint) publish(listenAddr string) {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		return
	}
	url := "http://127.0.0.1:" + port + mcpserver.Path
	e.url.Store(&url)
}

func (e *mcpEndpoint) get() string {
	if url := e.url.Load(); url != nil {
		return *url
	}
	return ""
}

// publicEndpoint is where a session on somebody's MAC reaches this endpoint:
// the deployment's gateway-fronted /api/mcp. A plain string, because nothing
// about it is discovered at boot — it is config (server.public_base_url), known
// before anything dispatches. Empty means the deployment was never told its own
// public name, which reads downstream as "no endpoint to hand out".
type publicEndpoint string

func (p publicEndpoint) get() string { return string(p) }

type mcpAddress interface{ get() string }

// claudeCodeMCP is the executor's claudecode.MCPProvider: one token per run,
// revoked when the run ends.
type claudeCodeMCP struct {
	endpoint mcpAddress
	tokens   *mcpserver.RunTokenRegistry
	// registry decides which tools this run's policy is served, so the executor
	// can name them in the session's system prompt. Nil in the minting-only
	// tests, whose manifest is then empty.
	registry port.ToolRegistry
	// ttl ceilings a credential that leaves this machine; "the run ended" is a
	// fact only this process observes. 0 means none, which is what a loopback
	// token gets.
	ttl time.Duration
	// now is time.Now, overridden in tests.
	now func() time.Time
}

var _ claudecode.MCPProvider = (*claudeCodeMCP)(nil)

func (m *claudeCodeMCP) clock() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

func (m *claudeCodeMCP) ForRun(ctx context.Context, run claudecode.MCPRun) (claudecode.MCPConfig, func(), error) {
	noop := func() {}
	url := m.endpoint.get()
	if url == "" {
		// Loopback: the listener has not bound yet, which the activation
		// ordering prevents. A task run that requires tools FAILS here rather
		// than degrading — a run without board tools cannot move its card or
		// tick a criterion, so it ends "successful", the criteria gate refuses,
		// and the board re-dispatches the same task in a silent, billable loop.
		// A chat turn (and a remote task, see RemoteExecutor.Execute) keeps the
		// old degrade-without-tools behaviour.
		if run.RequiresTools {
			log.Error().Str("task_key", run.Label).
				Msg("mcp endpoint address not published yet; refusing to start an agent cli task run that would have no tasktrooper board tools")
			return claudecode.MCPConfig{}, noop, fmt.Errorf(
				"the tasktrooper tool endpoint is not serving yet, so this %s run would have no way to move its card, "+
					"tick an acceptance criterion or record a verdict; it will be dispatched again once the server is up", run.Label)
		}
		log.Warn().Str("session_id", run.Label).
			Msg("no mcp endpoint address for this session; it runs on its native tools only, with no tasktrooper board tools")
		return claudecode.MCPConfig{}, noop, nil
	}

	// ctx is the caller's run context, so the endpoint attributes this
	// session's tool calls exactly as the agent loop would. run.Policy decides
	// the served tools; SkillsOnDisk rides the credential (and narrows the
	// surface — load_skill is withheld for workspace-file skills).
	served := mcpserver.Run{
		Ctx:          ctx,
		Policy:       run.Policy,
		TaskKey:      run.Label,
		SkillsOnDisk: run.SkillsOnDisk,
		ExpiresAt:    m.expiry(),
	}
	token, err := m.tokens.Mint(served)
	if err != nil {
		return claudecode.MCPConfig{}, noop, err
	}
	// The SAME Run value serves tools/list and the session's manifest, so the
	// model is never told it has a tool the endpoint will refuse.
	return claudecode.MCPConfig{
		URL:   url,
		Token: token,
		Tools: mcpserver.ServedToolNames(m.registry, served),
	}, func() { m.tokens.Revoke(token) }, nil
}

func (m *claudeCodeMCP) expiry() time.Time {
	if m.ttl <= 0 {
		return time.Time{}
	}
	return m.clock().Add(m.ttl)
}

// mcpTokenGrace stays valid past a session's own run timeout because minting
// and running are not the same clock: a session starts late (queueing, the
// tunnel, CLI start-up) and an equal ceiling would 401 healthy sessions in
// their last minutes — a silent failure this endpoint's logging exists to
// catch. It is short because the real revocation is the run context's
// cancellation and a process restart; this is the backstop for a copy that
// outlived all of those.
const mcpTokenGrace = 15 * time.Minute
