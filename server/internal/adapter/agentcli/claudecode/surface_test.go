package claudecode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The session must see THIS run's tool endpoint and no other. Without the pin
// the CLI merges the operator's personal MCP configuration in, and a board run
// is handed a stranger's servers — plus enough tools to push the CLI over the
// threshold where it stops sending schemas up front, which is what made a run
// spend ten turns searching for a tool it already had.
func TestSessionIsPinnedToItsOwnMCPConfig(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MCP: MCPConfig{URL: "http://127.0.0.1:8080/mcp", Token: "run-token"}}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Contains(t, argv, "--strict-mcp-config")
	assert.Less(t, indexOf(t, argv, "--mcp-config"), indexOf(t, argv, "--strict-mcp-config"),
		"the pin belongs with the config it pins")
}

// A run with no endpoint has nothing to pin, and passing the flag alone would
// only disable configuration the operator meant to have.
func TestNoMCPConfigMeansNoPin(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	assert.NotContains(t, readArgv(t, workDir), "--strict-mcp-config")
}

// Every prompt in this system names tools the way the registry does
// (list_acceptance_criteria), which is not the name the session can call. The
// manifest is what closes that gap, and it has to carry the exact string: a
// model that has been handed mcp__tasktrooper__list_acceptance_criteria has
// nothing left to search for.
func TestToolManifestNamesTheServedToolsAsTheSessionSeesThem(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MCP: MCPConfig{
		URL:   "http://127.0.0.1:8080/mcp",
		Token: "run-token",
		Tools: []string{"list_acceptance_criteria", "set_criterion_completed"},
	}}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	systemPrompt := argv[indexOf(t, argv, "--append-system-prompt")+1]
	assert.Contains(t, systemPrompt, "mcp__tasktrooper__list_acceptance_criteria")
	assert.Contains(t, systemPrompt, "mcp__tasktrooper__set_criterion_completed")
	assert.Contains(t, systemPrompt, "You are the backend developer.", "the persona still comes first")
}

// A run whose endpoint served nothing gets no manifest rather than an empty
// promise: "these are the tools you have" followed by nothing reads as a fault
// the model then reports instead of working.
func TestNoServedToolsMeansNoManifest(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MCP: MCPConfig{URL: "http://127.0.0.1:8080/mcp", Token: "run-token"}}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.NotContains(t, argv[indexOf(t, argv, "--append-system-prompt")+1], "mcp__tasktrooper__")
}

// A resumed session already holds the tool definitions, and the resume path
// deliberately sends no system prompt at all — see Execute. The manifest must
// not be what puts one back.
func TestResumedSessionKeepsItsEmptySystemPrompt(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MCP: MCPConfig{
		URL:   "http://127.0.0.1:8080/mcp",
		Token: "run-token",
		Tools: []string{"list_acceptance_criteria"},
	}}, "success.jsonl")

	req := taskExecution(workDir)
	req.ResumeSessionID = "sess-abc123"
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	assert.NotContains(t, readArgv(t, workDir), "--append-system-prompt")
}

// The board run that started without its own tool server is stopped where it
// starts. Left to run it does real work on the CLI's native tools, returns a
// plausible summary, is refused by the criteria gate for an untouched checklist
// and is dispatched again — silently, and for as long as the subscription
// lasts.
func TestSessionWithoutTheToolServerIsStopped(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MCP: MCPConfig{URL: "http://127.0.0.1:8080/mcp", Token: "run-token"}}, "foreign_mcp_only.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), mcpServerName)
	assert.Contains(t, err.Error(), "clickup=connected", "the message names what the session DID load")
}

// A CLI build that does not report its servers says nothing about whether the
// endpoint loaded, so it must not be read as saying no — every run would fail
// on the day that field moves or disappears.
func TestUnreportedServerListIsNotAFault(t *testing.T) {
	guard := &initGuard{label: "tt-42", require: true, cancel: func() {}}

	guard.OnInit(sessionInit{SessionID: "sess-1", Tools: []string{"Read"}})

	assert.NoError(t, guard.fault)
}

// The manifest is prompt text, so its two halves are asserted directly: the
// rule (what the prefix is) and the list (which tools this run holds).
func TestToolManifestStatesThePrefixRuleAndTheList(t *testing.T) {
	manifest := toolManifest([]string{"move_board_task"})

	assert.Contains(t, manifest, "mcp__tasktrooper__")
	assert.Contains(t, manifest, "mcp__tasktrooper__move_board_task")
	assert.Empty(t, toolManifest(nil))
	assert.Equal(t, "", withToolManifest("", []string{"move_board_task"}), "a resumed session's empty prompt stays empty")
}

// A local runner is somebody's working machine, and their ~/.claude carries
// their hooks and plugins. Loaded into a board run they rewrite the agent's
// instructions or block a tool call nobody is there to approve — and the result
// reads as the agent misbehaving.
func TestOperatorSettingsAreNotLoadedByDefault(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, DefaultSettingSources, argv[indexOf(t, argv, "--setting-sources")+1])
}

// The escape hatch for the install whose Claude auth lives in a user-level
// settings file.
func TestSettingSourcesAreConfigurable(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{SettingSources: "user,project,local"}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "user,project,local", argv[indexOf(t, argv, "--setting-sources")+1])
}

// The CLI validates this flag at startup, so a typo passed through would fail
// every run on the host with an error about an argument no operator typed.
func TestSettingSourcesDropWhatTheCLIDoesNotKnow(t *testing.T) {
	assert.Equal(t, "user", normalizeSettingSources(" USER , bogus , user "))
	assert.Equal(t, DefaultSettingSources, normalizeSettingSources("nonsense"))
	assert.Equal(t, DefaultSettingSources, normalizeSettingSources(""))
}
