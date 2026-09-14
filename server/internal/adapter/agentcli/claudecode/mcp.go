package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// MCPConfig points the CLI session at TaskTrooper's own tool surface: the board
// tools (move_board_task, review_criterion, add_task_comment, the PR tools) and
// the semantic-index tools, which the CLI does not ship with and which a
// claude_code run needs to close a task the way a loop run does. It is served by
// internal/adapter/mcpserver.
//
// With no MCPConfig and no MCPProvider set the session runs on the CLI's NATIVE
// tools only — file reading, editing and the shell, which are what a developer
// task mostly needs and what the CLI is best at. The board-side effects are then
// performed by the runner around it (commit, PR, column advance), as they were
// before this existed.
type MCPConfig struct {
	// URL is the MCP endpoint. Loopback for a session this process spawned
	// (http://127.0.0.1:8080/mcp); the public, gateway-fronted address for a
	// session on a member's Mac (https://<public base>/api/mcp), which is the
	// only form a machine on a home network can resolve.
	URL string
	// Token is sent as a bearer credential. It is a per-run secret: it goes
	// into a file — one this process writes and deletes for a local session,
	// one the runner writes and deletes for a remote one — and never onto the
	// command line, because a command line is world-readable in `ps`.
	Token string
	// Tools are the names the endpoint will serve this run, in the order
	// tools/list returns them. They go into the session's system prompt (see
	// toolManifest) so the model is TOLD what it holds instead of hunting for
	// it. Empty means the caller did not compute them and the manifest is
	// omitted rather than guessed at.
	Tools []string
}

func (c MCPConfig) set() bool { return c.URL != "" }

// MCPRun is everything the endpoint needs to know about the caller it is
// minting a credential for.
//
// It is a two-field struct rather than the domain.TaskExecution it used to be
// because a chat turn needs the same credential and is not a task: passing a
// TaskExecution with its History, WorkDir and TaskTitle left blank would have
// been a lie in the shape of a type. What the endpoint actually reads is the
// policy (which tools to serve) and a name for its logs, and that is now all it
// is given.
type MCPRun struct {
	// Policy decides which tools this session is served — the same policy the
	// agent loop would have enforced, applied at the point of execution.
	Policy domain.ToolPolicy
	// Label identifies the caller in the endpoint's logs: a board task's key
	// (tt-42) or a chat session's id. Never used for authorisation — the token
	// is.
	Label string
	// RequiresTools says this run cannot do its job without TaskTrooper's tools,
	// so a session that would get none must fail instead of starting.
	//
	// It is set for a TASK and not for a chat, and the asymmetry is the point. A
	// chat turn with no board tools is a worse conversation; a board run with no
	// board tools is a run that cannot finish the workflow at all — it cannot
	// move its card, cannot tick an acceptance criterion, cannot record a
	// review verdict. It still LOOKS like a normal run: the CLI does real work
	// on its native tools and returns a plausible closing message, the criteria
	// gate then refuses the hand-off because nothing was ticked, and the board
	// dispatches the same task again. That loop is silent, unbounded and
	// expensive — one qa-agent spent 6.3M input tokens of a flat-rate
	// subscription going round it — which is why this is an error and not a
	// warning. A run that fails in one sentence is dispatched once.
	RequiresTools bool
	// SkillsOnDisk says this run's skills are already files in its workspace —
	// application/agentfs wrote them there in the shape the CLI discovers on its
	// own — so the endpoint withholds load_skill from it. The tool would be a
	// second, worse path to content the session can already see.
	//
	// Like RequiresTools it is set for a materialised TASK and not for a chat,
	// and the asymmetry is again the point — but it fails in the opposite
	// direction, so both settings have a cost:
	//
	//   - set when the skills are NOT on disk (a chat turn, or any run nothing
	//     materialised for) it silently removes the only way that session can
	//     read a skill. Nothing reports it: the model is told at most that the
	//     tool is unavailable, concludes the agent has no skills, and does the
	//     work without them. The run looks normal and is simply worse, which is
	//     exactly the failure that survives review.
	//   - left unset when the skills ARE on disk it gives the session two
	//     mechanisms for one job, the classic way to make a model pick the worse
	//     one: it pays a JSON-RPC round trip per skill to re-read a file already
	//     in its own progressive-disclosure index.
	//
	// So it must follow the materialisation and nothing else. The board runner
	// decides both from one condition (cliFlavor) and carries it here on
	// domain.TaskExecution; see application/board/runner.go.
	//
	// create_skill is unaffected either way — writing a skill is not reading
	// one, and prompt.SkillsOnDiskMessage still asks for it.
	SkillsOnDisk bool
}

// MCPProvider gives ONE run its own endpoint and credential.
//
// The executor holds this rather than a fixed MCPConfig because the credential
// is per run, not per process: it is minted when the session starts and revoked
// when it ends, so a token that outlived its run — in a config file on a host
// that crashed, in a log, in a copied command line — authenticates nothing. It
// is also what lets the endpoint serve that run's own tool policy: the token is
// how the server knows which run is calling.
//
// "Run" means one board task OR one chat turn. A chat turn mints and revokes
// its own token exactly like a task does — the thread may be long-lived, but the
// credential must not be: between two messages there is no session working and
// therefore nothing that should be able to call this server's board tools.
//
// Implemented in platform/runtime, which is the only place that knows both the
// address this server bound and the token registry.
type MCPProvider interface {
	// ForRun mints the run's endpoint. ctx is the run's context — the same one
	// Execute or ExecuteChat was called with — because the endpoint executes
	// tool calls under it, so that a CLI session's tool call is attributed
	// exactly like a loop run's.
	//
	// The returned release is ALWAYS non-nil, including on error, so the caller
	// can defer it unconditionally. Calling it must be idempotent.
	ForRun(ctx context.Context, run MCPRun) (MCPConfig, func(), error)
}

// resolveMCP picks the config for one run: the provider's per-run one when
// there is a provider, and the executor's static one otherwise (which is what
// the tests use, and what an installation with no MCP endpoint gets — an empty
// config, meaning native tools only).
func (e *Executor) resolveMCP(ctx context.Context, run MCPRun) (MCPConfig, func(), error) {
	return resolveMCP(ctx, e.mcpProvider, e.mcp, run)
}

// resolveMCP is the same question for either executor, and it is a free
// function so the local and the remote one cannot answer it differently.
//
// The release is ALWAYS non-nil, including on error, so every caller can defer
// it unconditionally — which is what makes "the token dies with the run" true
// on the failure paths as well as the happy one.
func resolveMCP(ctx context.Context, provider MCPProvider, static MCPConfig, run MCPRun) (MCPConfig, func(), error) {
	noop := func() {}
	if provider == nil {
		return static, noop, nil
	}
	cfg, release, err := provider.ForRun(ctx, run)
	if release == nil {
		release = noop
	}
	if err != nil {
		return MCPConfig{}, release, err
	}
	return cfg, release, nil
}

// mcpServerName is what the tools appear as inside the session
// (mcp__tasktrooper__move_board_task and so on), so it must stay stable: a
// rename would invalidate every tool-name pattern a policy or a prompt refers
// to.
const mcpServerName = "tasktrooper"

// toolNamePrefix is what the CLI renames a served tool to inside the session.
// The endpoint knows the tool as list_acceptance_criteria; the model can only
// call it as mcp__tasktrooper__list_acceptance_criteria.
const toolNamePrefix = "mcp__" + mcpServerName + "__"

// toolManifest is the paragraph appended to a session's system prompt naming,
// exactly, the TaskTrooper tools that session holds.
//
// It exists because every prompt in this codebase — the role prompts, the
// hand-off rules, the runner's own instructions — names tools the way the
// registry does (set_criterion_completed, move_board_task), which is not the
// name the session can call. That mismatch was survivable while the CLI put
// every tool's schema in the context up front: the model saw the real names and
// used them. It stopped being survivable once a session crossed the CLI's
// tool-count threshold and the tools came behind ToolSearch, because then the
// only names the model had to search WITH were the unprefixed ones it had been
// given, and a search for a name that does not exist returns nothing however
// many times it is retried.
//
// So the manifest states both halves: the prefix rule, and the list. A model
// that has been handed the exact string has nothing left to look up.
func toolManifest(names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefixed := make([]string, 0, len(names))
	for _, name := range names {
		prefixed = append(prefixed, toolNamePrefix+name)
	}
	return "TaskTrooper's own tools reach you through the `" + mcpServerName + "` MCP server, so their real names carry the `" +
		toolNamePrefix + "` prefix: the tool this system's instructions call `set_criterion_completed` is called as `" +
		toolNamePrefix + "set_criterion_completed`. They are already available to you — do NOT search for them and do not report one as missing " +
		"because an unprefixed name did not resolve. These are the ones this run has, in full:\n" +
		strings.Join(prefixed, ", ") + "."
}

// withToolManifest appends the manifest to a system prompt.
//
// An empty prompt is left empty on purpose: that is the resumed-session case
// (see Execute), where the conversation already holds the tool definitions and
// anything added here would read as a fresh instruction on half-finished work.
func withToolManifest(systemPrompt string, names []string) string {
	manifest := toolManifest(names)
	if manifest == "" || strings.TrimSpace(systemPrompt) == "" {
		return systemPrompt
	}
	return systemPrompt + "\n\n" + manifest
}

// writeMCPConfigFile renders the per-run --mcp-config file and returns its path
// with a cleanup function. The cleanup is always non-nil, so the caller can
// defer it unconditionally.
//
// The file is written with 0600 into the OS temp dir rather than into the task
// workspace: the workspace is a git checkout the agent commits from, and a
// bearer token dropped in it is one `git add -A` away from being pushed.
func writeMCPConfigFile(cfg MCPConfig) (string, func(), error) {
	noop := func() {}
	if !cfg.set() {
		return "", noop, nil
	}
	headers := map[string]string{}
	if cfg.Token != "" {
		headers["Authorization"] = "Bearer " + cfg.Token
	}
	body, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			mcpServerName: map[string]any{
				"type":    "http",
				"url":     cfg.URL,
				"headers": headers,
			},
		},
	})
	if err != nil {
		return "", noop, fmt.Errorf("render mcp config: %w", err)
	}
	f, err := os.CreateTemp("", "tt-claude-mcp-*.json")
	if err != nil {
		return "", noop, fmt.Errorf("create mcp config file: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("secure mcp config file: %w", err)
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("write mcp config file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close mcp config file: %w", err)
	}
	return path, cleanup, nil
}
