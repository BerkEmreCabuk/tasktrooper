package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// newTestExecutor builds an executor pointed at testdata/fake-agy.sh and a
// workspace primed with the given fixture — see claudecode's identical helper
// for why the fixture lives IN the workspace rather than behind an env var.
func newTestExecutor(t *testing.T, cfg Config, fixtureFile string) (*Executor, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "fake-agy.sh"))
	require.NoError(t, err)

	workDir := t.TempDir()
	if fixtureFile != "" {
		body, err := os.ReadFile(filepath.Join("testdata", fixtureFile))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"), body, 0o600))
	}

	cfg.Binary = script
	ex, err := New(cfg)
	require.NoError(t, err)
	return ex, workDir
}

func readArgv(t *testing.T, workDir string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(workDir, "argv.txt"))
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(body), "\x00"), "\x00")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func indexOf(t *testing.T, values []string, want string) int {
	t.Helper()
	for i, v := range values {
		if v == want {
			return i
		}
	}
	t.Fatalf("%q not found in %v", want, values)
	return -1
}

func taskExecution(workDir string) domain.TaskExecution {
	return domain.TaskExecution{
		History: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are the backend developer."},
			{Role: domain.RoleUser, Content: "Implement the executor seam."},
		},
		Model:    "gemini-3.1-pro-high",
		Provider: domain.LLMProviderAntigravity,
		WorkDir:  workDir,
		TaskKey:  "tt-42",
	}
}

// A finished session must come back as the same response shape the agent loop
// returns, with the run's spend on the context accumulator the board runner
// stamps onto the row, AND with every native tool call it made recorded into
// the tool ledger the board's grounding gates read (trace.go's traceSink —
// see claudecode's identical wiring). success.jsonl's session calls
// read_file once successfully and run_terminal once with an error; both
// names are already canonical (trace.go's nativeToolNames is an identity
// map for names AGY happens to share with TaskTrooper's own tools), so this
// also doubles as proof the ledger names, not just the counts, are right.
func TestExecuteReturnsTheSessionsAnswer(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	ctx, toolUsage := registry.ContextWithToolUsage(context.Background())
	ctx, tokens := usageapp.ContextWithTokenUsage(ctx)

	resp, err := ex.Execute(ctx, taskExecution(workDir))
	require.NoError(t, err)

	assert.Equal(t, domain.RoleAssistant, resp.Message.Role)
	assert.Equal(t, "Added the executor seam and wired it in. Build and vet are green.", resp.Message.Content,
		"the result event's own response is the run's answer, not the last text_delta")

	totals := tokens.Totals()
	assert.Equal(t, 1, totals.LLMCalls)
	assert.Equal(t, int64(1500), totals.PromptTokens)
	assert.Equal(t, int64(800), totals.CompletionTokens)
	assert.Equal(t, int64(200), totals.CacheReadTokens)

	calls, failures := toolUsage.Totals()
	assert.Equal(t, 2, calls, "the successful read_file and the failed run_terminal both count")
	assert.Equal(t, 1, failures)
	assert.Equal(t, 1, toolUsage.Count("read_file"))
}

// The invocation is the contract with the CLI: every flag buildArgs documents
// has to actually reach the command line.
func TestExecuteBuildsTheDocumentedInvocation(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "-p", argv[0], "print mode has to come first")
	assert.Contains(t, argv, "--output-format")
	assert.Contains(t, argv, "stream-json")
	assert.Contains(t, argv, "--dangerously-skip-permissions", "nobody is at this terminal to approve an edit")
	assert.Contains(t, argv, "--model")
	assert.Contains(t, argv, "gemini-3.1-pro-high")

	// There is no separate system-prompt flag — system blocks are folded ahead
	// of the rest of the prompt, all as the one positional -p argument.
	prompt := argv[1]
	assert.Contains(t, prompt, "You are the backend developer.")
	assert.Contains(t, prompt, "Implement the executor seam.")
	assert.True(t, strings.Index(prompt, "You are the backend developer.") < strings.Index(prompt, "Implement the executor seam."),
		"system content has to sit ahead of the task, not after it")
}

// An empty model hands the choice to AGY's own configured default; the flag
// must be omitted entirely rather than sent empty.
func TestExecuteOmitsModelWhenNoneIsChosen(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.Model = ""
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	assert.NotContains(t, readArgv(t, workDir), "--model")
}

// The child must see a toolchain, not this process's secrets: childEnv forwards
// only PATH and HOME, so nothing else this test process carries should reach
// the fake CLI's environment file.
func TestExecuteScrubsTheChildEnvironment(t *testing.T) {
	t.Setenv("INTERNAL_AUTH_KEY", "gateway-hmac-secret")
	t.Setenv("DATABASE_URL", "postgres://user:pw@host/db")

	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")
	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	env := readFile(t, filepath.Join(workDir, "env.txt"))
	assert.NotContains(t, env, "INTERNAL_AUTH_KEY", "the gateway key must never reach a child process")
	assert.NotContains(t, env, "DATABASE_URL")
	assert.NotContains(t, env, "gateway-hmac-secret")
	assert.Contains(t, env, "PATH=", "a child with no PATH cannot run a single build command")
	assert.Contains(t, env, "HOME=")
}

func TestNewRefusesAMissingBinary(t *testing.T) {
	_, err := New(Config{Binary: "agy-that-is-not-installed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found on PATH")
}

// The workspace is the only tree this run may touch. A default would put the
// session in the shared project root, on whatever branch it happens to be on.
func TestExecuteRefusesToRunWithoutAWorkspace(t *testing.T) {
	ex, _ := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution("")
	_, err := ex.Execute(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no task workspace")
}

// Supports is what the runner asks before it picks a path. It must answer for
// exactly one provider, and answer safely when there is no executor at all.
func TestSupportsOnlyAntigravity(t *testing.T) {
	ex, _ := newTestExecutor(t, Config{}, "success.jsonl")

	assert.True(t, ex.Supports(domain.LLMProviderAntigravity))
	assert.False(t, ex.Supports(domain.LLMProviderClaudeCode))
	assert.False(t, ex.Supports(domain.LLMProviderAnthropic))

	var missing *Executor
	assert.False(t, missing.Supports(domain.LLMProviderAntigravity), "a nil executor supports nothing")
}

// AGY has no --mcp-config flag: the config is a file written into the
// workspace at a fixed path ahead of the run, and removed the moment the run
// ends — the bearer token inside it must not outlive the subprocess.
func TestMCPConfigIsWrittenForTheSessionAndRemovedAfter(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MCP: MCPConfig{URL: "http://127.0.0.1:8080/mcp", Token: "run-token"}}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	handed := readFile(t, filepath.Join(workDir, "mcp-config.json"))
	assert.Contains(t, handed, "http://127.0.0.1:8080/mcp")
	assert.Contains(t, handed, "Bearer run-token")

	argv := readArgv(t, workDir)
	assert.NotContains(t, argv, "run-token", "a command line is world-readable in ps")

	_, statErr := os.Stat(filepath.Join(workDir, ".agents", "mcp_config.json"))
	assert.True(t, os.IsNotExist(statErr), "the config file must not outlive the session")
}

// With no MCPConfig set, the session runs on AGY's native tools only — no
// config file is ever written.
func TestNoMCPConfigMeansNoFile(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(workDir, ".agents", "mcp_config.json"))
	assert.True(t, os.IsNotExist(statErr))
}

// recordingMCPProvider is a stand-in for platform/runtime's token minter —
// see claudecode's identical fake for the fuller reasoning.
type recordingMCPProvider struct {
	cfg      MCPConfig
	err      error
	minted   int
	released int
	run      MCPRun
}

func (p *recordingMCPProvider) ForRun(_ context.Context, run MCPRun) (MCPConfig, func(), error) {
	p.run = run
	if p.err != nil {
		return MCPConfig{}, nil, p.err
	}
	p.minted++
	return p.cfg, func() { p.released++ }, nil
}

func TestMCPProviderMintsAndRevokesPerRun(t *testing.T) {
	provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "per-run-secret"}}
	ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	assert.Equal(t, 1, provider.minted)
	assert.Equal(t, 1, provider.released, "the token dies with the run")
	assert.Equal(t, "tt-42", provider.run.Label)
}

// Every exit path releases, not just the happy one.
func TestMCPTokenIsReleasedOnEveryExitPath(t *testing.T) {
	t.Run("crashed session", func(t *testing.T) {
		provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "crashed"}}
		ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "")
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("1"), 0o600))

		_, err := ex.Execute(context.Background(), taskExecution(workDir))
		require.Error(t, err)
		assert.Equal(t, 1, provider.released)
	})

	t.Run("minting failed", func(t *testing.T) {
		provider := &recordingMCPProvider{err: errors.New("no entropy")}
		ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

		_, err := ex.Execute(context.Background(), taskExecution(workDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no entropy")
		assert.Equal(t, 0, provider.released, "nothing was minted, so there is nothing to release")
		_, statErr := os.Stat(filepath.Join(workDir, "argv.txt"))
		assert.True(t, os.IsNotExist(statErr), "a run without its credential must not start the CLI at all")
	})
}

// A session with no result event is a killed or crashed process — unlike
// opencode, AGY has no "clean exit is success" carve-out, so this must always
// be a hard failure.
func TestExecuteFailsWhenTheSessionNeverFinished(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"), []byte(
		`{"event":"init","conversation_id":"conv-x","init":{"cwd":"/workspace"}}`+"\n"+
			`{"event":"step_update","conversation_id":"conv-x","step_update":{"step_index":0,"state":"ACTIVE","step_type":"agent_response","text_delta":"working on it"}}`,
	), 0o600))

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ended without a result")
}

// A result event whose status is not SUCCESS is a failure, reported with the
// CLI's own status.
func TestExecuteFailsOnANonSuccessResultStatus(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"), []byte(
		`{"event":"result","conversation_id":"conv-x","result":{"status":"FAILED","response":"could not apply the patch"}}`,
	), 0o600))

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FAILED")
	assert.Contains(t, err.Error(), "could not apply the patch")
}

// A wedged CLI is the one failure nothing else in the system can see; the
// deadline is the only thing that ends it, and it must end it as a FAILURE —
// parking would wait out the window and then hand the same hang another hour.
func TestRunTimeoutFailsTheRunAndDoesNotPark(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{RunTimeout: 50 * time.Millisecond}, "success.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "sleep_seconds"), []byte("30"), 0o600))

	start := time.Now()
	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second, "the deadline has to actually kill the session")
	assert.Contains(t, err.Error(), "did not finish within")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "a hang is not a spent quota")
}

// A caller's own cancellation (stop button, pod drain) is reported as itself,
// not dressed up as this executor's deadline.
func TestCallerCancellationIsNotReportedAsATimeout(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{RunTimeout: time.Hour}, "success.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "sleep_seconds"), []byte("30"), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := ex.Execute(ctx, taskExecution(workDir))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "did not finish within")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
