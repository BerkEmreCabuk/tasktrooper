// Package mcpserver serves TaskTrooper's own domain tools to a headless Claude
// Code session over MCP.
//
// It exists because of the gap named in adapter/agentcli/claudecode: a CLI
// session runs on the CLI's native tools, which know how to read and edit a
// repository and nothing at all about a board. Without this endpoint a
// claude_code run cannot move its card, tick an acceptance criterion, comment on
// its own pull request or search the semantic index — every board-side effect
// has to be performed by the runner around it, and the run's tool policy is a
// statement rather than a rule.
//
// # Why hand-rolled and not the SDK
//
// This repository already depends on github.com/modelcontextprotocol/go-sdk and
// uses it as a CLIENT (adapter/mcp) to talk to other people's servers. It is not
// used here, for four reasons that all point the same way:
//
//   - the HTTP surface is Fiber/fasthttp, not net/http. The SDK's
//     StreamableHTTPHandler is an http.Handler, so it would have to come in
//     through fasthttpadaptor, which buffers the response — that is, we would
//     pay for the SDK's SSE and session machinery and then have to switch both
//     off (Stateless + JSONResponse) to make it work;
//   - authentication and the TOOL SURFACE ITSELF are per run. The tools a
//     session may see depend on that run's domain.ToolPolicy, so the SDK's
//     per-request getServer hook would have to build (or cache) a whole
//     *mcp.Server per bearer token;
//   - Server.AddTool PANICS on any tool whose schema is not an object schema.
//     Our schemas come from ~60 independent tool executors, so a single odd one
//     would take down a request goroutine rather than degrade;
//   - the SDK re-resolves and validates every input schema before the handler
//     runs. The registry's executors already validate their own arguments, and a
//     second validator in between can only reject calls the first would have
//     accepted.
//
// What is left to implement is small and fixed: one client (the local CLI), one
// transport (streamable HTTP), and the five methods that client sends —
// initialize, notifications/initialized, tools/list, tools/call, ping. There is
// no SSE and no resumability because there is nothing to push: every tool call
// is a request with a response.
package mcpserver

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Path is where the endpoint is mounted. A child process on this host reaches
// it at http://127.0.0.1:<the server's own port>/mcp; a session on a member's
// Mac reaches the same handler through the control plane at
// <public base>/api/mcp, which the gateway forwards here with the /api prefix
// stripped (see PublicPath).
//
// Deliberately NOT under /v1 or /admin: those two prefixes are what
// adapter/http's auth middlewares gate (see isPublicPath), and this endpoint
// authenticates itself with the per-run bearer token instead. Putting it under
// /v1 would demand the API key or a gateway signature from a child
// process that has neither — and must never be given either, since the whole
// point of the env scrub is that the CLI session holds no credential of this
// server's.
const Path = "/mcp"

// PublicPath is the same endpoint as the OUTSIDE world addresses it: the
// control plane proxies /api/* to this deployment and strips the prefix before
// forwarding, so a Mac posting to <public base>/api/mcp arrives here on Path.
//
// The /api prefix belongs to another repository, and it is spelled out here for
// the reason adapter/runner spells out /internal/runner/forward/ rather than
// importing it: the two programs are deployed independently and a shared
// constant would be a shared module neither of them wants. A change to it is a
// coordinated multi-repo change, like the internalauth header names.
const PublicPath = "/api" + Path

// PublicURL is where a session on somebody's Mac must POST to reach this
// endpoint. Empty base (server.public_base_url unset) returns empty, which the
// caller reads as "there is no address to hand out" rather than building a
// relative URL no client can resolve.
func PublicURL(publicBaseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + PublicPath
}

// serverName is what the session sees this server called. It has to stay in
// step with the key claudecode writes into the per-run --mcp-config file: the
// CLI namespaces tools as mcp__<key>__<tool>, and a rename would invalidate
// every tool-name pattern a prompt or a policy refers to.
const serverName = "tasktrooper"

const serverVersion = "1.0.0"

// defaultProtocolVersion is what initialize answers with when the client asks
// for a revision this server does not know.
const defaultProtocolVersion = "2025-03-26"

// knownProtocolVersions are the published spec revisions this endpoint will
// echo back. The method set implemented here is identical in all of them, so
// agreeing with the client costs nothing and avoids a pointless downgrade;
// anything outside this set is answered with defaultProtocolVersion, which is
// what the spec requires of a server that does not support what was asked.
var knownProtocolVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
	"2025-11-25": true,
}

// JSON-RPC 2.0 error codes. The last one is in the implementation-defined range
// (-32000..-32099) and is only ever paired with HTTP 401.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeUnauthorized   = -32001
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	// ID is kept raw and echoed byte for byte: JSON-RPC allows a string or a
	// number, and re-typing it risks handing back an id the client cannot match
	// to its own request. Absent means the message is a notification.
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Server is the endpoint. One instance serves every concurrent run; the runs are
// told apart by their bearer tokens alone.
type Server struct {
	registry port.ToolRegistry
	tokens   *RunTokenRegistry
	// loopbackOnly refuses clients that are not on this host. See
	// SetLoopbackOnly.
	loopbackOnly bool
}

func New(registry port.ToolRegistry, tokens *RunTokenRegistry) *Server {
	return &Server{registry: registry, tokens: tokens}
}

// SetLoopbackOnly restricts the endpoint to clients connecting from this
// machine. It follows WHERE THE SESSION RUNS, which is not the same question as
// whether this is a cloud deployment:
//
//   - local executor in a cloud pod — ON. The pod binds 0.0.0.0, which puts
//     /mcp on the pod network, and unlike every other route on that port /mcp
//     is deliberately outside the /v1 and /admin prefixes the gateway auth
//     middlewares gate (see Path). The one legitimate client is a `claude`
//     child this server started, on this host, at 127.0.0.1 — so the address is
//     checked as well as the token and a leaked token buys nothing from
//     anywhere else in the cluster.
//   - remote executor — OFF, necessarily. The session is on somebody's Mac and
//     calls in through the control plane; every legitimate request then arrives
//     from the gateway's address and a loopback check would refuse all of them.
//     The token is the whole of the authentication on that path, which is why
//     it is minted per run, expires, and dies with the
//     run (see Run and RunTokenRegistry).
//   - desktop / self-hosted — either; the listener binds 127.0.0.1 and the
//     kernel already refuses everyone else.
//
// Set from configuration rather than passed to New so the existing call sites —
// and the tests, which drive the handler through fiber's in-memory transport and
// therefore have no real peer address — stay as they are.
func (s *Server) SetLoopbackOnly(enabled bool) { s.loopbackOnly = enabled }

// Register mounts the endpoint.
//
// POST is the transport. GET and DELETE are mounted only so they answer 405
// instead of falling through to whatever is registered after them — the SPA's
// catch-all would otherwise answer a protocol request with index.html, and a
// client that got HTML where it expected JSON reports a confusing parse error
// rather than "this server does not do that".
func (s *Server) Register(router fiber.Router) {
	router.Post(Path, s.handlePost)
	router.Get(Path, s.methodNotAllowed)
	router.Delete(Path, s.methodNotAllowed)
}

// methodNotAllowed answers the two verbs the spec defines and this server does
// not implement: GET opens a server->client SSE stream (there is nothing to
// push: every call is a request with a response) and DELETE ends a session
// (there are no sessions — a run's lifetime is the token's, not a session id's).
func (s *Server) methodNotAllowed(c *fiber.Ctx) error {
	return c.Status(fiber.StatusMethodNotAllowed).JSON(rpcResponse{
		JSONRPC: "2.0",
		ID:      json.RawMessage("null"),
		Error:   &rpcError{Code: codeInvalidRequest, Message: "this MCP endpoint only accepts POST"},
	})
}

func (s *Server) handlePost(c *fiber.Ctx) error {
	// Address before token: a caller that has no business on this endpoint is
	// told so without its bearer being looked up, so a token-guessing sweep from
	// elsewhere in the cluster learns nothing from timing either.
	if !s.callerAllowed(c) {
		log.Warn().Str("remote_ip", c.IP()).
			Msg("mcp: refused a call from outside this host; the only legitimate client is a local claude code session")
		return c.Status(fiber.StatusForbidden).JSON(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeInvalidRequest, Message: "this MCP endpoint only serves processes on the server host"},
		})
	}

	run, ok := s.authenticate(c)
	if !ok {
		// 401 AND a JSON-RPC error body: the status is what an HTTP client
		// keys off, the body is what an MCP client shows.
		//
		// Logged at WARN, not debug, and this is the whole reason a real outage
		// went unseen for a day. Claude Code turns a 401 on a tools/call into
		// `MCP server "tasktrooper" requires re-authorization (token expired)`
		// and gives up on the server for the rest of the session — so the run
		// carries on with no board tools, writes its verdicts as prose, fails
		// the criteria gate and is dispatched again. Every symptom of that is on
		// the AGENT's side; the only thing the server ever knew about it was one
		// debug line nobody had enabled.
		//
		// The token prefix and the live count are what tell the two causes
		// apart: a token this process never minted (it belongs to a session
		// started before a restart — the registry is in memory) versus one it
		// minted and has since revoked.
		log.Warn().
			Str("token_prefix", tokenPrefix(c.Get(fiber.HeaderAuthorization))).
			Int("live_runs", s.liveRuns()).
			Str("method", peekMethod(c.Body())).
			Msg("mcp: rejected a call with an unknown or revoked run token; that session has lost its tasktrooper tools for the rest of its run")
		c.Set(fiber.HeaderWWWAuthenticate, `Bearer realm="tasktrooper-mcp"`)
		return c.Status(fiber.StatusUnauthorized).JSON(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeUnauthorized, Message: "unknown or expired run token"},
		})
	}

	var req rpcRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		// A batch (a JSON array) lands here too. Claude Code never sends one,
		// and JSON-RPC batching was removed from the spec in the 2025-06-18
		// revision, so it is refused rather than half-implemented.
		return c.Status(fiber.StatusBadRequest).JSON(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeParseError, Message: "request body is not a JSON-RPC 2.0 message"},
		})
	}

	// No id means a notification: the spec says accept it with 202 and no body.
	// notifications/initialized is the one that always arrives; the rest
	// (cancelled, progress, roots/list_changed) are accepted and ignored, which
	// is exactly what a server with no server->client stream can do with them.
	if len(req.ID) == 0 {
		// Send(nil) rather than SendStatus, which would put the status TEXT in
		// the body — a client that parses whatever comes back reads "Accepted"
		// as a malformed JSON-RPC message.
		return c.Status(fiber.StatusAccepted).Send(nil)
	}

	result, rpcErr := s.dispatch(run, req)
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
	return c.JSON(resp)
}

// callerAllowed reports whether the client's address may reach this endpoint.
//
// c.IP() is the peer's own address, not a header: the fiber app is built without
// ProxyHeader (see platform/runtime), so nothing a client can send changes the
// answer. That matters here — a check that trusted X-Forwarded-For would be one
// header away from being no check at all.
func (s *Server) callerAllowed(c *fiber.Ctx) bool {
	if !s.loopbackOnly {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(c.IP()))
	// An unparseable address is refused rather than allowed: this gate only runs
	// where the endpoint is on a shared network, and "I could not tell where
	// this came from" is not a reason to serve the board tools.
	return ip != nil && ip.IsLoopback()
}

// tokenPrefix is the first few characters of a presented bearer, enough to
// correlate a rejection with the mint that produced it in the same log and not
// enough to reconstruct the credential.
func tokenPrefix(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "<no bearer>"
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "<empty>"
	}
	if len(token) > 8 {
		return token[:8] + "…"
	}
	return token
}

// peekMethod reads the JSON-RPC method out of a body this handler is about to
// refuse, so the rejection line says whether the session lost its tools at
// connect time (tools/list, and the model then reports the tool as missing) or
// mid-run (tools/call, which the CLI reports as an expired token).
func peekMethod(body []byte) string {
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		return "?"
	}
	return req.Method
}

func (s *Server) liveRuns() int {
	if s.tokens == nil {
		return 0
	}
	return s.tokens.Live()
}

// authenticate resolves the Authorization header to a live run.
func (s *Server) authenticate(c *fiber.Ctx) (Run, bool) {
	if s.tokens == nil {
		return Run{}, false
	}
	const prefix = "Bearer "
	header := c.Get(fiber.HeaderAuthorization)
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return Run{}, false
	}
	return s.tokens.Lookup(strings.TrimSpace(header[len(prefix):]))
}

func (s *Server) dispatch(run Run, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params), nil
	case "ping":
		// The spec's keepalive: an empty result object, nothing else.
		return struct{}{}, nil
	case "tools/list":
		tools := servedTools(s.registry, run)
		s.logExposedTools(run, tools)
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		return s.callTool(run, req.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "unsupported method: " + req.Method}
	}
}

// logExposedTools states, once per CLI session, how much of TaskTrooper that
// session actually got.
//
// tools/list is asked exactly once per invocation — the CLI does initialize,
// notifications/initialized, tools/list and then nothing but tools/call — so
// this is one line per session and it is the answer to the only question that
// matters when a run behaves as though the board did not exist: was it served
// the tools, or was it not. Until this existed the answer was recoverable only
// from the agent's own prose ("bu ortamda list_acceptance_criteria yüklü
// değil"), which is both late and unreliable.
//
// The names go to debug: thirty tool names on every session start is noise in a
// log that is read for something else, and the count is what distinguishes the
// three states worth telling apart.
func (s *Server) logExposedTools(run Run, tools []toolInfo) {
	if len(tools) == 0 && !run.Policy.IsZero() {
		// The bug state. A policy that names tools and a surface that has none
		// means the two disagree — a registry that has not been populated, or an
		// allowlist that matches nothing that is registered — and the session is
		// about to run as if it had no board at all.
		log.Warn().
			Str("task_key", run.TaskKey).
			Int("policy_tools", len(run.Policy.AllowTools)).
			Msg("mcp: this claude code session is being served NO tasktrooper tools even though its run has a tool policy; it cannot move its card or tick a criterion")
		return
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	log.Info().
		Str("task_key", run.TaskKey).
		Int("tools", len(tools)).
		Msg("mcp: serving tasktrooper tools to a claude code session")
	log.Debug().
		Str("task_key", run.TaskKey).
		Strs("tool_names", names).
		Msg("mcp: tasktrooper tools served to this session")
}

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	// A malformed params object is not worth failing on: the version is the only
	// field read, and its fallback is the same one an absent field gets.
	_ = json.Unmarshal(params, &p)

	version := defaultProtocolVersion
	if knownProtocolVersions[p.ProtocolVersion] {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		// Tools only, and listChanged false: the surface is fixed for the whole
		// run because the run's policy is. Advertising prompts or resources we
		// do not serve would earn a round trip per capability for an empty list.
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{"name": serverName, "version": serverVersion},
	}
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) callTool(run Run, params json.RawMessage) (any, *rpcError) {
	var p callToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call params are not an object"}
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call needs a tool name"}
	}

	args := strings.TrimSpace(string(p.Arguments))
	if args == "" || args == "null" {
		// The registry's executors unmarshal this string; an empty one is how a
		// no-argument call is spelled everywhere else in this codebase.
		args = "{}"
	}

	// The run's context, not the HTTP request's: the call is the RUN's, and every
	// decorator between here and the tool (audit, action ledger, tool-usage
	// counters, the activity recorder) reads its attribution off that context.
	// It also carries the run's cancellation, so a stopped run's in-flight tool
	// call dies with it.
	ctx := run.Ctx

	// The trace is opened BEFORE the gate below, so a call this endpoint refuses
	// is as visible as one it runs. A session that spent a turn on a tool its
	// policy denies is exactly what a human reading the run needs to see; a
	// silent refusal reads on the board as a turn that did nothing.
	id := callID()
	traceStart(ctx, id, name, args)

	// The same gate tools/list is built from, applied again here. Filtering the
	// advertised list is not access control: a client is free to call a name it
	// was never given, and without this check a session could reach run_terminal
	// or a tool its policy denies just by asking for it.
	if !s.available(name, run) {
		refusal := s.unavailable(name, run)
		traceResult(ctx, id, name, refusalText(refusal), true)
		return refusal, nil
	}

	result := s.registry.ExecuteWithPolicy(ctx, domain.ToolCall{
		ID:       id,
		Type:     "function",
		Function: domain.FunctionCall{Name: name, Arguments: args},
	}, run.Policy)
	traceResult(ctx, id, name, result.Content, result.IsError)

	log.Debug().
		Str("task_key", run.TaskKey).
		Str("tool", name).
		Bool("is_error", result.IsError).
		Msg("mcp: claude code session called a tasktrooper tool")

	return resultToMCP(name, result), nil
}

// available reports whether this endpoint will execute name for run.
//
// It walks the very list tools/list is rendered from, so the advertised surface
// and the executable one cannot disagree about what the session may do. That
// second check is not belt-and-braces: withholding a tool from tools/list hides
// it, and a client is free to call a name it was never given, so a run could
// otherwise reach run_terminal — or the load_skill its workspace already
// answers — just by asking for it.
func (s *Server) available(name string, run Run) bool {
	for _, tool := range servedTools(s.registry, run) {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// unavailable answers a call for a tool this endpoint does not serve, and says
// WHICH of the five reasons it is — a model that is told "not allowed" when the
// real answer is "you misspelled it" retries the same call.
//
// isError rather than a JSON-RPC error, for the reason on callToolResult.IsError:
// the model is the one that picked the name, so the model is who needs to read
// that it was the wrong one. A protocol error would be shown to the operator
// and leave the session guessing.
func (s *Server) unavailable(name string, run Run) callToolResult {
	switch {
	case name == domain.AskUserToolName:
		return textResult(name+" is not available in a Claude Code session: it parks the run waiting for a human answer, which this session cannot wait for. Decide with the information you have, or say what is missing in your final message.", true)
	case run.SkillsOnDisk && name == skillLoadTool:
		// Named where to go instead, because this is the one refusal whose
		// content the session still needs: told only "not available", a model
		// that was about to apply a skill concludes it has none.
		return textResult(name+" is not served to this run: its skills are already installed in this workspace and your own skill mechanism lists them, so read the one you want from there. create_skill is still available if you need to write a new skill.", true)
	case !exposed(name, run.SkillsOnDisk):
		return textResult(name+" is not served here — use your own built-in tool for that (Bash, Read, Write, Edit, Grep, Glob).", true)
	case !s.registered(name):
		return textResult("no tool called "+name+" exists. Call tools/list for the ones this run has.", true)
	default:
		return textResult(name+" is not available to this run: its tool policy does not allow it.", true)
	}
}

func (s *Server) registered(name string) bool {
	for _, known := range s.registry.AllToolNames() {
		if known == name {
			return true
		}
	}
	return false
}
