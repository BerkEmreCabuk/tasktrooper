package runtime

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/agentcli/claudecode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// mcpEndpoint publishes the loopback URL a CHILD process on this host must POST
// to in order to reach this server's MCP endpoint.
//
// It exists because of an ordering problem, not an abstraction one: the Claude
// Code executor is constructed inside buildHandler, which also activates the
// board workers, so a run can be executing before buildHandler returns — while
// with Server.Port 0 (desktop, and every test) the port is not even chosen
// until the listener binds. Run therefore binds FIRST and publishes the bound
// address into this before it builds the handler, so the address is a fact
// rather than a guess by the time anything can read it.
//
// The host is always 127.0.0.1, never whatever the listener bound. A cloud pod
// listens on 0.0.0.0, which is not an address anything can connect TO, and the
// only client this URL is ever handed to is a process on this same host.
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

// publicEndpoint is the address a session on somebody's MAC reaches this
// endpoint at: the deployment's gateway-fronted /api/mcp.
//
// A plain string rather than the atomic above, because nothing about it is
// discovered at boot — it is configuration (server.public_base_url), known
// before anything can dispatch. An empty one is a deployment that was never
// told its own public name, which is a real state and reads downstream as "no
// endpoint to hand out" rather than as a URL nobody can resolve.
type publicEndpoint string

func (p publicEndpoint) get() string { return string(p) }

// mcpAddress is the one thing the provider needs from either of the two above.
type mcpAddress interface{ get() string }

// claudeCodeMCP is the executor's claudecode.MCPProvider: one token per run,
// revoked when the run ends.
type claudeCodeMCP struct {
	endpoint mcpAddress
	tokens   *mcpserver.RunTokenRegistry
	// registry answers WHICH tools this run's policy is served, so the executor
	// can name them in the session's system prompt. Nil in the tests that only
	// exercise minting, which then get no manifest — the same as an install
	// whose registry has not been wired.
	registry port.ToolRegistry
	// offHost says the credential this provider mints LEAVES this machine: it
	// is written into a config file in somebody's home directory and presented
	// from there over the public internet. Two rules follow from it, and
	// neither is worth applying to a loopback token:
	//
	//   - the tenant must be known. A loopback token that somehow carried none
	//     would fail closed at the first database statement on this same
	//     process; one that travels is a credential this process is publishing,
	//     and publishing an unscoped one is not a thing to do and then discover
	//     later. So minting is refused instead.
	//   - the token expires. Its run's own lifetime bounds it either way, but
	//     "the run ended" is a fact only this process observes; the absolute
	//     ceiling is the part that holds if the copy on the laptop outlives
	//     everything else.
	offHost bool
	// ttl is that ceiling. 0 means none, which is what a loopback token gets.
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
		// Loopback: the listener has not bound yet. Only reachable if a run
		// started before the HTTP server did, which the activation ordering
		// prevents. Remote: server.public_base_url is not configured, so there
		// is no address a laptop could resolve.
		//
		// A TASK fails here rather than degrading — when it asked to. It used
		// to degrade unconditionally, on the reasoning that a run without the
		// board tools still does real work on the CLI's native ones — which is
		// true and beside the point: the work it cannot do is the workflow. It
		// cannot move its card, tick a criterion or record a verdict, so the
		// run ends looking successful, the criteria gate refuses the hand-off,
		// and the board dispatches the same task again. Nothing in that loop
		// reports a fault and nothing ends it; it just spends the
		// subscription. One sentence on the run row is cheaper than any number
		// of laps. See MCPRun.RequiresTools — and see RemoteExecutor.Execute
		// for why the remote path deliberately does NOT set it.
		if run.RequiresTools {
			log.Error().Str("task_key", run.Label).
				Msg("mcp endpoint address not published yet; refusing to start a claude code task run that would have no tasktrooper board tools")
			return claudecode.MCPConfig{}, noop, fmt.Errorf(
				"the tasktrooper tool endpoint is not serving yet, so this %s run would have no way to move its card, "+
					"tick an acceptance criterion or record a verdict; it will be dispatched again once the server is up", run.Label)
		}
		// A chat turn keeps the old behaviour: fewer tools is a worse
		// conversation, not an unfinishable one. So does a remote task, whose
		// alternative is failing every run of every claude_code agent on one
		// unset configuration line.
		log.Warn().Str("session_id", run.Label).
			Msg("no mcp endpoint address for this session; it runs on its native tools only, with no tasktrooper board tools")
		return claudecode.MCPConfig{}, noop, nil
	}

	// WHOSE run this is, read once, here, and bound to the credential.
	//
	// It is the only place the answer is available: /mcp is a public path (its
	// caller holds no gateway signature and no tenant API key, by design), so
	// no signed X-Internal-Tenant reaches the endpoint and tenantMiddleware
	// never runs for it. ctx is the board runner's runCtx, which carries the
	// dispatching tenant (board.RunJob.Tenant), so minting is the moment where
	// "which tenant" is still a fact rather than a claim.
	//
	// uuid.Nil is the single-tenant case — a self-hosted or desktop install
	// whose run context carries no identity — and is passed through unchanged
	// for the loopback path. For a credential that leaves this machine it is
	// refused: see claudeCodeMCP.offHost.
	id, _ := tenant.From(ctx)
	if m.offHost && id.TenantID == uuid.Nil {
		log.Error().Str("task_key", run.Label).
			Msg("refusing to mint an mcp run token with no tenant; a credential that leaves this machine must name exactly one tenant")
		return claudecode.MCPConfig{}, noop, fmt.Errorf(
			"this %s run has no tenant on its context, so no tasktrooper tool credential can be issued for it", run.Label)
	}

	// ctx is the caller's run context — the board runner's runCtx, or a chat
	// turn's: the endpoint executes this session's tool calls under it, so they
	// are attributed exactly as the agent loop's would be. run.Policy is the
	// same policy the loop would have enforced, and it decides which tools the
	// session is served. A chat turn is credentialled identically to a board
	// run, which is the point — the agent has TaskTrooper's tools in
	// conversation for the same reason it has them on a card.
	// SkillsOnDisk rides along for the same reason: it narrows the surface too
	// (load_skill is withheld from a run whose skills are files in its
	// workspace), so the endpoint has to learn it from the credential exactly as
	// it learns the policy.
	served := mcpserver.Run{
		Ctx:          ctx,
		Tenant:       id,
		Policy:       run.Policy,
		TaskKey:      run.Label,
		SkillsOnDisk: run.SkillsOnDisk,
		ExpiresAt:    m.expiry(),
	}
	token, err := m.tokens.Mint(served)
	if err != nil {
		return claudecode.MCPConfig{}, noop, err
	}
	// ONE Run value decides both what the endpoint will serve this token and
	// what the session is told it holds, because ServedToolNames answers the
	// question off the same struct tools/list will. A manifest built from a
	// second, hand-assembled notion of the run would eventually name a tool the
	// endpoint refuses — and the model, having been told it has it, would keep
	// calling it.
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

// mcpTokenGrace is how long past a session's own run timeout its credential
// stays valid.
//
// It is not zero because the two clocks are not the same clock: the ceiling is
// stamped in this process when the token is minted, and the session on the Mac
// starts some time after that (queueing behind another of that member's runs,
// the tunnel round trip, the CLI's own start-up) and finishes on its own
// deadline. A ceiling equal to the run timeout would start expiring healthy
// sessions in their last minutes — a 401 the CLI reports as
// `requires re-authorization (token expired)` and then gives up on the server
// for the rest of the run, which is the exact silent failure this whole
// endpoint's logging exists to catch.
//
// It is short because the token's real revocation is elsewhere: the executor's
// defer, the run context's cancellation, and a process restart all kill it
// sooner. This is only the backstop for a copy that outlived all three.
const mcpTokenGrace = 15 * time.Minute
