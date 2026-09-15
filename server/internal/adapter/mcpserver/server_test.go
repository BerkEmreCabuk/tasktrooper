package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeRegistry stands in for the real tool registry: it filters on the policy's
// AllowTools the way registry.FilterToolNames does for plain (non-MCP) names,
// and records what it was asked to execute and under which context.
type fakeRegistry struct {
	defs    []domain.ToolDefinition
	results map[string]domain.ToolResult

	mu       sync.Mutex
	executed []domain.ToolCall
	lastCtx  context.Context
}

var _ port.ToolRegistry = (*fakeRegistry)(nil)

func (r *fakeRegistry) Register(port.ToolExecutor) {}

func (r *fakeRegistry) Definitions() []domain.ToolDefinition {
	return r.DefinitionsForPolicy(domain.ToolPolicy{})
}

func (r *fakeRegistry) DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition {
	if len(policy.AllowTools) == 0 {
		return r.defs
	}
	allowed := make(map[string]bool, len(policy.AllowTools))
	for _, name := range policy.AllowTools {
		allowed[name] = true
	}
	out := make([]domain.ToolDefinition, 0, len(r.defs))
	for _, def := range r.defs {
		if allowed[def.Function.Name] {
			out = append(out, def)
		}
	}
	return out
}

func (r *fakeRegistry) AllToolNames() []string {
	names := make([]string, 0, len(r.defs))
	for _, def := range r.defs {
		names = append(names, def.Function.Name)
	}
	return names
}

func (r *fakeRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (r *fakeRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, _ domain.ToolPolicy) domain.ToolResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executed = append(r.executed, call)
	r.lastCtx = ctx
	if result, ok := r.results[call.Function.Name]; ok {
		return result
	}
	return domain.ToolResult{Name: call.Function.Name, Content: "ok"}
}

func (r *fakeRegistry) calls() []domain.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.ToolCall(nil), r.executed...)
}

func def(name string) domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        name,
			Description: name + " description",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"q": map[string]any{"type": "string"}},
			},
		},
	}
}

// fullCatalog mixes the three groups the endpoint has to tell apart: tools the
// CLI covers natively, the parking tool, and the ones only TaskTrooper has.
func fullCatalog() []domain.ToolDefinition {
	return []domain.ToolDefinition{
		def("run_terminal"), def("read_file"), def("write_file"), def("edit_file"),
		def("edit_lines"), def("delete_file"), def("move_file"), def("grep_code"),
		def("get_repo_tree"),
		def(domain.AskUserToolName),
		def("move_board_task"), def("review_criterion"), def("add_task_comment"),
		def("codebase_search"), def("get_symbol_skeleton"), def("expand_symbol_context"),
		def("mobile_screenshot"),
		// The skill pair: served or withheld per RUN rather than per tool, and
		// never together — see the tests in surface_test.go.
		def("load_skill"), def("create_skill"),
	}
}

// newTestServer wires a server with one live run and returns the app, the fake
// registry and that run's token.
func newTestServer(t *testing.T, reg *fakeRegistry, run Run) (*fiber.App, *RunTokenRegistry, string) {
	t.Helper()
	if run.Ctx == nil {
		run.Ctx = context.Background()
	}
	tokens := NewRunTokenRegistry()
	token, err := tokens.Mint(run)
	require.NoError(t, err)

	app := fiber.New()
	New(reg, tokens).Register(app)
	return app, tokens, token
}

// call posts one JSON-RPC message and returns the HTTP response plus the parsed
// body (nil when there is none).
func call(t *testing.T, app *fiber.App, token, body string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// What Claude Code's streamable-HTTP client actually sends. Answering it
	// with plain application/json is allowed and is what this server does.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if len(raw) == 0 {
		return resp, nil
	}
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed), "body was not JSON: %s", raw)
	return resp, parsed
}

func toolNames(t *testing.T, body map[string]any) []string {
	t.Helper()
	result, ok := body["result"].(map[string]any)
	require.True(t, ok, "no result in %v", body)
	list, ok := result["tools"].([]any)
	require.True(t, ok, "no tools in %v", result)
	names := make([]string, 0, len(list))
	for _, entry := range list {
		names = append(names, entry.(map[string]any)["name"].(string))
	}
	return names
}

func callResultOf(t *testing.T, body map[string]any) (blocks []any, isError bool) {
	t.Helper()
	require.Nil(t, body["error"], "expected a tool result, got a protocol error: %v", body["error"])
	result, ok := body["result"].(map[string]any)
	require.True(t, ok, "no result in %v", body)
	content, _ := result["content"].([]any)
	flag, _ := result["isError"].(bool)
	return content, flag
}

// The handshake. The version is negotiated, the capabilities say tools and
// nothing else, and the name is the one the CLI namespaces tools under.
func TestInitialize(t *testing.T) {
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()}, Run{})

	resp, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"claude-code","version":"1.0"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", strings.Split(resp.Header.Get("Content-Type"), ";")[0])

	result := body["result"].(map[string]any)
	assert.Equal(t, "2025-06-18", result["protocolVersion"], "a version this server knows is echoed rather than downgraded")
	assert.Equal(t, "tasktrooper", result["serverInfo"].(map[string]any)["name"])
	caps := result["capabilities"].(map[string]any)
	assert.Contains(t, caps, "tools")
	assert.NotContains(t, caps, "resources", "advertising what we do not serve buys a round trip for an empty list")

	// An unknown revision gets one this server does support, which is what the
	// spec asks of it.
	_, body = call(t, app, token, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	assert.Equal(t, defaultProtocolVersion, body["result"].(map[string]any)["protocolVersion"])
}

// notifications/initialized carries no id, so there is nothing to answer: 202
// and an empty body, per the transport spec.
func TestNotificationsAreAccepted(t *testing.T) {
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()}, Run{})

	resp, body := call(t, app, token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Nil(t, body)
}

func TestPingAndUnknownMethod(t *testing.T) {
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()}, Run{})

	_, body := call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	assert.Nil(t, body["error"])
	assert.Equal(t, map[string]any{}, body["result"])

	_, body = call(t, app, token, `{"jsonrpc":"2.0","id":2,"method":"resources/list"}`)
	require.NotNil(t, body["error"])
	assert.Equal(t, float64(codeMethodNotFound), body["error"].(map[string]any)["code"])
}

// The tool surface is the point of the whole endpoint: everything the CLI
// already does better is gone, everything only TaskTrooper has is there, and
// ask_user is not offered at all.
func TestToolsListDropsNativelyCoveredToolsAndAskUser(t *testing.T) {
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()}, Run{})

	_, body := call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	names := toolNames(t, body)

	for _, native := range []string{"run_terminal", "read_file", "write_file", "edit_file",
		"edit_lines", "delete_file", "move_file", "grep_code", "get_repo_tree"} {
		assert.NotContains(t, names, native, "%s is what the CLI's own tool is for", native)
	}
	assert.NotContains(t, names, domain.AskUserToolName,
		"ask_user parks the run on a human answer, which a live session cannot wait for")

	assert.Contains(t, names, "move_board_task")
	assert.Contains(t, names, "review_criterion")
	assert.Contains(t, names, "add_task_comment")
	for _, semantic := range []string{"codebase_search", "get_symbol_skeleton", "expand_symbol_context"} {
		assert.Contains(t, names, semantic, "%s is index-backed; the CLI has no equivalent", semantic)
	}

	assert.True(t, sortedAscending(names), "an order that changes per request churns the CLI's prompt: %v", names)

	// The schema travels as-is, because the registry's is the one the tool
	// actually validates against.
	tools := body["result"].(map[string]any)["tools"].([]any)
	first := tools[0].(map[string]any)
	assert.Equal(t, "object", first["inputSchema"].(map[string]any)["type"])
	assert.NotEmpty(t, first["description"])
}

// The run's policy is the same one the agent loop would have enforced, and it
// decides the surface here too.
func TestToolsListAppliesTheRunsPolicy(t *testing.T) {
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()},
		Run{Policy: domain.ToolPolicy{AllowTools: []string{"move_board_task", "read_file"}}})

	_, body := call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	names := toolNames(t, body)

	assert.Equal(t, []string{"move_board_task"}, names,
		"the policy narrows the list, and read_file is still dropped as natively covered")
}

// A call is executed under the RUN's context, so everything downstream — the
// audit rows, the action ledger, the tool-usage counters the grounding gates
// read — attributes it to the run rather than to a stray HTTP request.
func TestToolsCallExecutesUnderTheRunsContext(t *testing.T) {
	reg := &fakeRegistry{
		defs:    fullCatalog(),
		results: map[string]domain.ToolResult{"move_board_task": {Name: "move_board_task", Content: "moved tt-42 to code_review"}},
	}
	taskID := uuid.New()
	runCtx := registry.ContextWithTaskID(registry.ContextWithWorkspaceDir(context.Background(), "/w/tt-42"), taskID)
	app, _, token := newTestServer(t, reg, Run{Ctx: runCtx, TaskKey: "tt-42"})

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"move_board_task","arguments":{"column":"code_review"}}}`)

	blocks, isError := callResultOf(t, body)
	assert.False(t, isError)
	require.Len(t, blocks, 1)
	assert.Equal(t, "text", blocks[0].(map[string]any)["type"])
	assert.Equal(t, "moved tt-42 to code_review", blocks[0].(map[string]any)["text"])
	assert.Equal(t, float64(7), body["id"], "the request id comes back untouched")

	calls := reg.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "move_board_task", calls[0].Function.Name)
	assert.JSONEq(t, `{"column":"code_review"}`, calls[0].Function.Arguments)
	assert.NotEmpty(t, calls[0].ID, "the registry stamps this onto the result and the audit row")

	assert.Equal(t, taskID, registry.TaskIDFromContext(reg.lastCtx))
	assert.Equal(t, "/w/tt-42", registry.WorkspaceDirFromContext(reg.lastCtx))
}

// A tool that fails comes back as isError, NOT as a JSON-RPC error: the model
// picked the arguments, so the model is who has to read what was wrong with
// them.
func TestToolFailuresAreToolErrorsNotProtocolErrors(t *testing.T) {
	reg := &fakeRegistry{
		defs:    fullCatalog(),
		results: map[string]domain.ToolResult{"review_criterion": {Name: "review_criterion", Content: "criterion 9 does not exist", IsError: true}},
	}
	app, _, token := newTestServer(t, reg, Run{})

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"review_criterion","arguments":{"index":9}}}`)

	blocks, isError := callResultOf(t, body)
	assert.True(t, isError)
	assert.Contains(t, blocks[0].(map[string]any)["text"], "criterion 9 does not exist")
}

// Screenshots have to arrive as pictures. A tool result carrying images maps
// onto MCP image content next to its text, not into a base64 wall of text.
func TestImagesBecomeImageContent(t *testing.T) {
	reg := &fakeRegistry{
		defs: fullCatalog(),
		results: map[string]domain.ToolResult{"mobile_screenshot": {
			Name:    "mobile_screenshot",
			Content: "screenshot of the login screen",
			Images: []domain.ToolResultImage{
				{MediaType: "image/png", Data: "aGVsbG8="},
				{Data: "d29ybGQ="},
			},
		}},
	}
	app, _, token := newTestServer(t, reg, Run{})

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mobile_screenshot","arguments":{}}}`)

	blocks, isError := callResultOf(t, body)
	assert.False(t, isError)
	require.Len(t, blocks, 3)

	assert.Equal(t, "text", blocks[0].(map[string]any)["type"])

	img := blocks[1].(map[string]any)
	assert.Equal(t, "image", img["type"])
	assert.Equal(t, "aGVsbG8=", img["data"])
	assert.Equal(t, "image/png", img["mimeType"])

	assert.Equal(t, "image/png", blocks[2].(map[string]any)["mimeType"], "a missing media type still has to render")
}

// Filtering the advertised list is not access control. A client is free to ask
// for a name it was never given, and every one of those names has to be refused
// at the point of execution too.
func TestToolsCallRefusesWhatToolsListWithheld(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, _, token := newTestServer(t, reg, Run{Policy: domain.ToolPolicy{AllowTools: []string{"move_board_task"}}})

	// The reason matters as much as the refusal: a model told "not allowed"
	// when it actually misspelled the name retries the same call.
	for name, want := range map[string]string{
		"run_terminal":          "use your own built-in tool",
		domain.AskUserToolName:  "parks the run",
		"add_task_comment":      "tool policy does not allow it",
		"a_tool_that_never_was": "no tool called a_tool_that_never_was exists",
	} {
		_, body := call(t, app, token,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":{}}}`)
		blocks, isError := callResultOf(t, body)
		assert.True(t, isError, "%s must be refused", name)
		assert.Contains(t, blocks[0].(map[string]any)["text"], want, "refusal for %s", name)
	}

	assert.Empty(t, reg.calls(), "a refused name must never reach the registry")
}

// The bearer token is the only thing between a local process and the board.
// Missing, wrong or revoked all end the same way, and nothing runs.
func TestBearerAuth(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, tokens, token := newTestServer(t, reg, Run{})
	const listCall = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	for _, tc := range []struct{ name, header string }{
		{"missing", ""},
		{"wrong token", "not-a-real-token"},
		{"empty bearer", " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := call(t, app, tc.header, listCall)
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			require.NotNil(t, body["error"])
			assert.Equal(t, float64(codeUnauthorized), body["error"].(map[string]any)["code"])
		})
	}

	t.Run("a live token works", func(t *testing.T) {
		resp, _ := call(t, app, token, listCall)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("revoked with the run", func(t *testing.T) {
		tokens.Revoke(token)
		resp, _ := call(t, app, token, listCall)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"the run is over; its credential must be worthless")
	})

	assert.Empty(t, reg.calls())
}

// A protocol request answered with HTML (the SPA fallback) reads as a parse
// error at the client, so the two verbs this server does not implement say so
// themselves.
func TestUnsupportedVerbsAnswerMethodNotAllowed(t *testing.T) {
	app, _, _ := newTestServer(t, &fakeRegistry{defs: fullCatalog()}, Run{})

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		resp, err := app.Test(httptest.NewRequest(method, Path, nil))
		require.NoError(t, err)
		assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode, method)
	}
}

func TestMalformedBodyIsAParseError(t *testing.T) {
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()}, Run{})

	resp, body := call(t, app, token, `{"jsonrpc":"2.0",`)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, float64(codeParseError), body["error"].(map[string]any)["code"])
}

func sortedAscending(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] > values[i] {
			return false
		}
	}
	return true
}

// In cloud mode the listener binds 0.0.0.0, which puts this route on the pod
// network — and it sits outside the prefixes the gateway auth middlewares gate,
// so without an address check a run token would be the only thing between the
// cluster and this server's board tools. The only legitimate client is a `claude`
// child on this host.
func TestCloudModeRefusesClientsThatAreNotOnThisHost(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, _, token := newTestServer(t, reg, Run{Policy: domain.ToolPolicy{}})

	// fiber's in-memory transport reports 0.0.0.0 as the peer, which is exactly
	// the "not a loopback address" case: a request that did not come from a
	// process on this machine.
	srv := New(reg, NewRunTokenRegistry())
	srv.SetLoopbackOnly(true)
	guarded := fiber.New()
	srv.Register(guarded)

	resp, body := call(t, guarded, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	assert.NotNil(t, body["error"], "the refusal is a JSON-RPC error the client can read")
	assert.Empty(t, reg.calls(), "a refused caller must not reach a tool")

	// The same request on a server that is not in cloud mode still works: a
	// desktop install binds 127.0.0.1, so the kernel is already the gate.
	resp, body = call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Nil(t, body["error"])
}

// The address is checked BEFORE the token, so a refused caller cannot use this
// endpoint to tell a live token from a dead one.
func TestCloudModeRefusesBeforeLookingAtTheToken(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	srv := New(reg, NewRunTokenRegistry())
	srv.SetLoopbackOnly(true)
	app := fiber.New()
	srv.Register(app)

	resp, _ := call(t, app, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode,
		"not 401: the caller is refused for where it is, before its credential is considered")
}
