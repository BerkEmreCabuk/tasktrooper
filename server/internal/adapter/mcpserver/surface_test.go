package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// These tests pin the three things that decide whether a Claude Code session
// has TaskTrooper's board tools at all. Each of them was a live hypothesis for a
// QA run that reported "list_acceptance_criteria is not loaded in this
// environment"; none of them was the cause, which is exactly why they are worth
// holding still.

// registeredTool is a minimal executor: enough to be registered and to have a
// definition, with no dependency on any store.
type registeredTool struct{ name string }

func (t registeredTool) Name() string { return t.name }
func (t registeredTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{
		Name:        t.name,
		Description: t.name,
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}
func (t registeredTool) Execute(context.Context, string) domain.ToolResult {
	return domain.ToolResult{Name: t.name, Content: "ok"}
}

var _ port.ToolExecutor = registeredTool{}

func listTools(t *testing.T, app *fiber.App, token string) map[string]bool {
	t.Helper()
	resp, body := call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw, err := json.Marshal(body["result"])
	require.NoError(t, err)
	var result struct {
		Tools []toolInfo `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	names := make(map[string]bool, len(result.Tools))
	for _, tl := range result.Tools {
		names[tl.Name] = true
	}
	return names
}

// The registry the endpoint is handed is the DECORATED one, built in
// platform/runtime before most tools exist: the board executors are registered
// several hundred lines further down. Every decorator has to hold its inner
// registry by reference for that to work — a snapshot taken at New would serve
// a session the tools that happened to exist at boot and none of the board's.
func TestToolsListSeesToolsRegisteredAfterNew(t *testing.T) {
	base := registry.New()
	base.Register(registeredTool{name: "codebase_search"})

	// The exact decorator stack platform/runtime builds (runtime.go: toolReg).
	decorated := registry.NewWorkspaceRegistry(
		registry.NewActionRecordingRegistry(
			registry.NewAuditingRegistry(base, nil),
			nil,
		),
		nil,
	)

	tokens := NewRunTokenRegistry()
	token, err := tokens.Mint(Run{Ctx: context.Background(), TaskKey: "tt-late"})
	require.NoError(t, err)
	app := fiber.New()
	New(decorated, tokens).Register(app)

	// board.NewExecutors runs after mcpserver.New in the real wiring.
	base.Register(registeredTool{name: "list_acceptance_criteria"})
	base.Register(registeredTool{name: "review_criterion"})

	names := listTools(t, app, token)
	assert.True(t, names["list_acceptance_criteria"],
		"a board tool registered after mcpserver.New must still be served")
	assert.True(t, names["review_criterion"])
	assert.True(t, names["codebase_search"])
}

// The policy a QA verification run carries into a verdict column must still
// contain the two tools a verdict is made of by the time it reaches toolsFrom.
//
// The QA role's own allowlist is pinned in application/catalog (see
// TestQARunPolicyKeepsTheVerdictTools there, which runs the same narrowing
// over the real role policy); what is pinned HERE is the second half — that a
// policy which allows them produces a surface that serves them.
//
// Both stage behaviours are in play and both are aimed at writers: an in_qa
// run's strip_writers loses the file tools and commit_task_changes, on top of
// whatever no_read_file/no_code_reading the stage also carries. Losing the
// criteria tools with them would leave a QA agent able to test but not to
// record, which is the exact shape of the failure this test exists for.
func TestQAVerdictPolicyKeepsTheCriteriaTools(t *testing.T) {
	qa := domain.ToolPolicy{AllowTools: []string{
		"run_terminal", "read_file", "grep_code", "get_repo_tree",
		"list_board_tasks", "move_board_task", "add_task_comment", "list_task_comments",
		"list_acceptance_criteria", "review_criterion", "get_pipeline_status",
	}}
	wf := workflowtest.Default().Workflows[domain.TaskType("task")]
	stage, ok := wf.Stage(domain.TaskColumnInQA)
	require.True(t, ok)
	policy := domain.RestrictToolsForStage(
		domain.UpliftWorkspaceTools(domain.MergeToolPolicy(domain.ToolPolicy{}, qa)),
		stage, wf.Type,
	)
	require.NotEmpty(t, policy.AllowTools, "a QA run's policy must not collapse to unrestricted")

	base := registry.New()
	for _, name := range []string{
		"list_acceptance_criteria", "review_criterion", "move_board_task",
		"add_task_comment", "list_task_comments", "get_pipeline_status",
		// Registered and allowed, but the CLI ships its own, so they must NOT
		// appear: their absence is by design, not a symptom.
		"read_file", "run_terminal", "grep_code", "get_repo_tree",
	} {
		base.Register(registeredTool{name: name})
	}

	tokens := NewRunTokenRegistry()
	token, err := tokens.Mint(Run{Ctx: context.Background(), Policy: policy, TaskKey: "tt-qa"})
	require.NoError(t, err)
	app := fiber.New()
	New(base, tokens).Register(app)

	names := listTools(t, app, token)
	assert.True(t, names["list_acceptance_criteria"], "QA cannot read criterion ids without it")
	assert.True(t, names["review_criterion"], "QA cannot record a verdict without it")
	assert.True(t, names["move_board_task"], "QA cannot leave in_qa without it")
	assert.False(t, names["read_file"], "natively covered tools stay off the MCP surface")
	assert.False(t, names["run_terminal"])
}

// A run token outlives the cancellation of the run's context on purpose.
//
// The token is minted for a CHILD PROCESS, and that process is alive for exactly
// as long as Execute has not returned — Execute is what revokes. Tying validity
// to the context instead would mean any cancellation (a stop, a deadline, a
// drain) turned the still-running session's next tool call into an HTTP 401,
// which Claude Code reports as `requires re-authorization (token expired)` and
// then stops using the server for the rest of the session. The call must fail as
// a cancelled call, with the reason, not as an authentication problem.
func TestTokenSurvivesRunContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reg := &fakeRegistry{defs: fullCatalog()}
	app, tokens, token := newTestServer(t, reg, Run{Ctx: ctx, TaskKey: "tt-cancelled"})

	cancel()

	run, ok := tokens.Lookup(token)
	require.True(t, ok, "cancelling the run context must not invalidate the session's credential")
	require.Error(t, run.Ctx.Err())

	resp, _ := call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"a cancelled run must not read to the CLI as an expired token")

	// And revoking — which is what Execute's defer does when the child has
	// actually exited — does invalidate it.
	tokens.Revoke(token)
	resp, _ = call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// An empty surface with a policy that names tools is the bug state the WARN
// exists for; it must not be answered as though it were a legitimately empty
// registry. The assertion here is on the surface itself — the log line is the
// operator-facing half of the same fact.
func TestEmptySurfaceWithANonEmptyPolicyIsAnEmptyList(t *testing.T) {
	base := registry.New()
	tokens := NewRunTokenRegistry()
	token, err := tokens.Mint(Run{
		Ctx:     context.Background(),
		Policy:  domain.ToolPolicy{AllowTools: []string{"list_acceptance_criteria"}},
		TaskKey: "tt-empty",
	})
	require.NoError(t, err)
	app := fiber.New()
	New(base, tokens).Register(app)

	assert.Empty(t, listTools(t, app, token))
}

// A board run on the claude_code provider has its skills written into its
// workspace by application/agentfs, in the shape the CLI discovers and lazily
// reads by itself. load_skill would then be a SECOND mechanism for the one job
// — a JSON-RPC round trip to fetch bytes already sitting in the session's own
// skill index — and handing a model two paths to the same content is the
// classic way to make it take the worse one.
//
// create_skill has to survive the same cut. Authoring a skill is not reading
// one: the CLI discovers skill files, it never writes them back into this
// agent's catalog, and prompt.SkillsOnDiskMessage still asks a self-evolving
// agent to save what the task forced it to work out. Withholding it here would
// turn self-evolution off for every board CLI run and look like symmetry.
func TestSkillsOnDiskRunIsNotServedTheSkillLoader(t *testing.T) {
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()},
		Run{TaskKey: "tt-42", SkillsOnDisk: true})

	names := listTools(t, app, token)

	assert.False(t, names["load_skill"],
		"this run's skills are files in its own workspace; the tool is a worse path to them")
	assert.True(t, names["create_skill"],
		"writing a skill is independent of reading one, and the prompt still asks for it")
	assert.True(t, names["move_board_task"], "the rest of the surface is untouched")
}

// Withholding a tool from tools/list HIDES it; it does not deny it. A client may
// call any name it likes, so a session that remembered load_skill from another
// run — or simply guessed it — would reach straight past the advertised list.
// The endpoint is careful about that distinction everywhere else (see
// TestToolsCallRefusesWhatToolsListWithheld) and this narrowing is no different.
//
// The refusal names where the skills actually are, because this is the one
// refusal whose content the session still needs: told only that the tool is
// unavailable, a model that was about to apply a skill concludes it has none.
func TestSkillsOnDiskRunIsRefusedTheSkillLoaderAtExecution(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, _, token := newTestServer(t, reg, Run{TaskKey: "tt-42", SkillsOnDisk: true})

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"load_skill","arguments":{"name":"hexagonal"}}}`)

	blocks, isError := callResultOf(t, body)
	assert.True(t, isError, "hiding it from tools/list is not access control")
	text := blocks[0].(map[string]any)["text"].(string)
	assert.Contains(t, text, "installed in this workspace", "the session is told where its skills are")
	assert.Contains(t, text, "create_skill", "and that authoring one is still open to it")
	assert.Empty(t, reg.calls(), "a refused name must never reach the registry")
}

// Nothing materialises a workspace for a chat turn — application/agentfs runs in
// the board runner and only for a task — so a chat session has no skill files to
// find and load_skill is its ONLY way to read one. Withholding it globally (by
// adding it to nativelyCovered, the naive version of this change) would take
// that away with no compile error and no log line: the agent would simply answer
// without its skills, in conversations that still look normal.
func TestChatTurnKeepsTheSkillLoader(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, _, token := newTestServer(t, reg, Run{TaskKey: "chat-7"})

	names := listTools(t, app, token)
	assert.True(t, names["load_skill"], "a chat turn has no skill files to read instead")
	assert.True(t, names["create_skill"])

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"load_skill","arguments":{"name":"hexagonal"}}}`)
	_, isError := callResultOf(t, body)
	assert.False(t, isError, "and it must execute, not just be advertised")
	require.Len(t, reg.calls(), 1)
	assert.Equal(t, "load_skill", reg.calls()[0].Function.Name)
}
