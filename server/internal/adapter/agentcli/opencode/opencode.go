// Package opencode delegates one board task to a headless OpenCode CLI
// session running on this host, the same pattern claudecode and antigravity
// use: the board runner still clones the repository, checks out the task
// branch, and does everything after the session (verify gate, commit, PR,
// column advance). OpenCode is handed a prepared workspace and gives back a
// closing message.
//
// OpenCode's own tool surface reaches TaskTrooper's board tools through
// OPENCODE_CONFIG_CONTENT (see mcp.go) — an env var carrying inline JSON the
// CLI merges over the project's own opencode.json — rather than a file or a
// per-invocation flag. There is no documented --max-turns equivalent and no
// separate system-prompt flag, so a run's whole history is folded into the
// one positional prompt `opencode run` takes.
package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// DefaultMaxConcurrent is how many OpenCode sessions may run at once.
const DefaultMaxConcurrent = 3

// DefaultRunTimeout bounds ONE session end to end.
const DefaultRunTimeout = time.Hour

// DefaultSlotWait bounds how long a run waits for a concurrency slot.
const DefaultSlotWait = 10 * time.Minute

const stderrTailMax = 8 << 10

// Config drives one Executor.
type Config struct {
	// Binary is the CLI to run; empty means "opencode", resolved on PATH.
	Binary string
	// MaxConcurrent caps simultaneous OpenCode sessions. <= 0 means
	// DefaultMaxConcurrent.
	MaxConcurrent int
	// RunTimeout bounds one session; <= 0 means DefaultRunTimeout.
	RunTimeout  time.Duration
	MCP         MCPConfig
	MCPProvider MCPProvider
}

// Executor runs board tasks through the OpenCode CLI. It satisfies
// port.TaskExecutor.
type Executor struct {
	bin         string
	runTimeout  time.Duration
	mcp         MCPConfig
	mcpProvider MCPProvider
	slots       chan struct{}
	slotWait    time.Duration
	now         func() time.Time
}

var _ port.TaskExecutor = (*Executor)(nil)

// New resolves the binary and returns the executor, or an error when the
// binary is not on PATH.
func New(cfg Config) (*Executor, error) {
	resolved, err := ResolveBinary(cfg.Binary)
	if err != nil {
		return nil, fmt.Errorf("opencode executor: %w", err)
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrent
	}
	runTimeout := cfg.RunTimeout
	if runTimeout <= 0 {
		runTimeout = DefaultRunTimeout
	}
	return &Executor{
		bin:         resolved,
		runTimeout:  runTimeout,
		mcp:         cfg.MCP,
		mcpProvider: cfg.MCPProvider,
		slots:       make(chan struct{}, maxConcurrent),
		slotWait:    DefaultSlotWait,
		now:         time.Now,
	}, nil
}

// Supports answers for the one provider this executor exists for.
func (e *Executor) Supports(provider domain.LLMProviderType) bool {
	return e != nil && provider == domain.LLMProviderOpencode
}

// Execute runs the task in an OpenCode session and maps its outcome onto the
// response shape the board runner already reads.
func (e *Executor) Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	if e == nil {
		return domain.AgentResponse{}, errors.New("opencode executor is not configured")
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		return domain.AgentResponse{}, errors.New("opencode executor: no task workspace to run in")
	}

	release, err := e.acquire(ctx)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	defer release()

	mcpCfg, releaseMCP, err := e.resolveMCP(ctx, MCPRun{Policy: req.Policy, Label: req.TaskKey})
	defer releaseMCP()
	if err != nil {
		return domain.AgentResponse{}, err
	}
	configContent, err := mcpConfigContentEnv(mcpCfg)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	s, err := e.spawn(ctx, invocation{
		workDir:       req.WorkDir,
		prompt:        flattenHistory(req.History),
		label:         req.TaskKey,
		configContent: configContent,
	})
	if err != nil {
		return domain.AgentResponse{}, err
	}
	return e.finish(ctx, req.TaskKey, s)
}

// invocation is one spawn of the CLI.
type invocation struct {
	workDir       string
	prompt        string
	label         string
	configContent string
	stream        port.ChatStream
}

// buildArgs renders the opencode command line for one spawn.
func (e *Executor) buildArgs(inv invocation) []string {
	return []string{
		"run", inv.prompt,
		"--format", "json",
		// Without this a run only PROPOSES edits and applies nothing; there
		// is no human at this terminal to approve them.
		"--auto",
	}
}

func (e *Executor) spawn(ctx context.Context, inv invocation) (session, error) {
	runCtx, cancel := context.WithTimeout(ctx, e.runTimeout)
	defer cancel()

	args := e.buildArgs(inv)
	cmd := exec.CommandContext(runCtx, e.bin, args...)
	cmd.Dir = inv.workDir
	cmd.Env = childEnv(ctx)
	if inv.configContent != "" {
		cmd.Env = append(cmd.Env, "OPENCODE_CONFIG_CONTENT="+inv.configContent)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return session{}, fmt.Errorf("opencode stdout: %w", err)
	}
	stderr := &tailWriter{max: stderrTailMax}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return session{}, fmt.Errorf("start opencode: %w", err)
	}

	out, parseErr := parseStream(stdout, newStreamingSink(&noopSink{}, inv.stream))
	if parseErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil

	return session{
		out:        out,
		stderrTail: stderr.String(),
		parseErr:   parseErr,
		waitErr:    waitErr,
		timedOut:   timedOut,
	}, nil
}

type session struct {
	out        outcome
	stderrTail string
	parseErr   error
	waitErr    error
	timedOut   bool
}

func (e *Executor) finish(ctx context.Context, label string, s session) (domain.AgentResponse, error) {
	out := s.out
	usageapp.TokenUsageFromContext(ctx).Add(out.Usage)

	if s.timedOut {
		return domain.AgentResponse{}, fmt.Errorf("opencode did not finish within %s and was stopped: %s",
			e.runTimeout, domain.TruncateHead(strings.TrimSpace(s.stderrTail), 500))
	}
	if s.parseErr != nil {
		return domain.AgentResponse{}, s.parseErr
	}
	// A known opencode issue (run --format json can exit cleanly without ever
	// emitting the final step_finish event) means a missing terminal event
	// with a clean exit and real output is treated as success, unlike
	// claudecode/antigravity where no terminal event is always a failure.
	if !out.SawResult {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.AgentResponse{}, ctxErr
		}
		if s.waitErr != nil || strings.TrimSpace(out.Text) == "" {
			return domain.AgentResponse{}, fmt.Errorf("opencode ended without a result (%v): %s",
				s.waitErr, domain.TruncateHead(strings.TrimSpace(s.stderrTail), 500))
		}
	}
	if out.IsError {
		return domain.AgentResponse{}, fmt.Errorf("opencode failed (%s): %s",
			out.Status, domain.TruncateHead(firstNonEmpty(out.Text, strings.TrimSpace(s.stderrTail)), 1000))
	}
	if s.waitErr != nil {
		return domain.AgentResponse{}, fmt.Errorf("opencode exited with an error after reporting success (%v): %s",
			s.waitErr, domain.TruncateHead(strings.TrimSpace(s.stderrTail), 500))
	}
	if strings.TrimSpace(out.Text) == "" {
		return domain.AgentResponse{}, errors.New("opencode finished without producing any answer")
	}
	return domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: out.Text},
		Usage:   out.Usage,
	}, nil
}

func (e *Executor) acquire(ctx context.Context) (func(), error) {
	select {
	case e.slots <- struct{}{}:
		return func() { <-e.slots }, nil
	default:
	}
	wait := e.slotWait
	if wait <= 0 {
		wait = DefaultSlotWait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case e.slots <- struct{}{}:
		return func() { <-e.slots }, nil
	case <-timer.C:
		return nil, fmt.Errorf("opencode executor is busy: no session slot came free within %s (limit %d concurrent sessions)",
			wait, cap(e.slots))
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// flattenHistory folds the runner's message list into the single positional
// prompt `opencode run` takes.
func flattenHistory(history []domain.Message) string {
	var parts []string
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if msg.Role == domain.RoleAssistant {
			content = "Earlier assistant turn:\n" + content
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

type noopSink struct{}

func (noopSink) OnSession(sessionID, model string)                       {}
func (noopSink) OnTurn()                                                 {}
func (noopSink) OnAssistantText(text string)                             {}
func (noopSink) OnToolUse(callID, name, arguments string)                {}
func (noopSink) OnToolResult(callID, name, content string, isError bool) {}

type tailWriter struct {
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.max > 0 && n > w.max {
		p = p[n-w.max:]
	}
	w.buf = append(w.buf, p...)
	if w.max > 0 && len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	return n, nil
}

func (w *tailWriter) String() string { return string(bytes.TrimSpace(w.buf)) }
