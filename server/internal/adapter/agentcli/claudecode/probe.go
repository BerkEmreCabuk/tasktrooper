package claudecode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ProbeResult is what a successful probe learned about this host's CLI.
type ProbeResult struct {
	// BinaryPath is the absolute path ResolveBinary found — the same one the
	// executor runs.
	BinaryPath string
	// Version is whatever `claude --version` printed, trimmed to one line. Kept
	// because a CLI behaving oddly is usually a CLI several minor versions
	// behind, and having the number on the connection row turns that from a
	// question into a fact.
	Version string
}

// probeVersionTimeout bounds `claude --version`. It starts a Node process and
// prints a string; anything slower than this is a broken install, not a slow
// one, and waiting longer only delays the report.
const probeVersionTimeout = 60 * time.Second

// DefaultProbeTimeout bounds the whole probe, including the one-turn session
// that proves the CLI is signed in. Generous because that session pays the
// CLI's full startup — settings, plugins, MCP servers the operator configured —
// before it ever reaches the model.
const DefaultProbeTimeout = 3 * time.Minute

// probePrompt is what the auth session is asked. It exists to make the CLI
// authenticate and answer, so it is the cheapest possible turn: no tool the
// session could reach, nothing about the workspace, nothing worth a second
// turn.
const probePrompt = "Reply with exactly: ok"

// authFailureMarkers are the substrings that mean "this binary has no usable
// session", matched case-insensitively against the CLI's own output.
//
// A list rather than one string because the CLI does not have one way of
// saying it: a never-logged-in install, an expired OAuth token and a rejected
// key each produce a different sentence, and all three are the same fix. It is
// deliberately NOT a catch-all — a failure that matches nothing here is
// reported as itself, with the CLI's output attached, because guessing "must be
// auth" for an unrecognised crash would send the operator to log in again and
// again while the actual fault sat in the message we discarded.
var authFailureMarkers = []string{
	"invalid api key",
	"invalid_api_key",
	"authentication_error",
	"authentication failed",
	"please run /login",
	"run /login",
	"claude login",
	"not logged in",
	"no credentials",
	"credentials not found",
	"oauth token has expired",
	"token has expired",
	"unauthorized",
	"401",
}

// Probe reports whether this host can actually run a Claude Code session: the
// binary is there, and it holds a subscription session.
//
// Both halves are checked by RUNNING the thing, because neither can be
// established any other way. A file at the resolved path proves nothing about
// whether it starts (a broken npm shim resolves fine and dies on exec), and no
// file on disk proves the CLI is signed in — the credential lives in the macOS
// Keychain on one host, in ~/.claude/.credentials.json on another, and in an
// environment variable on a third, and a connect that inspected the wrong one
// of those three would report a healthy connection for a CLI that cannot make
// a single call.
//
// So the auth half is a real one-turn session. It costs a few hundred tokens of
// the operator's subscription, once, at the moment they press a button that
// says "connect" — which is the trade this whole flow exists to make. The
// alternative is a green tick that means "a file existed".
//
// The two failures come back as distinguishable errors
// (domain.ErrAgentCLIBinaryMissing, domain.ErrAgentCLIUnauthenticated) because
// they have different fixes; see those sentinels.
func Probe(ctx context.Context, binary string, settingSources string) (ProbeResult, error) {
	resolved, err := ResolveBinary(binary)
	log.Info().Str("configured", binary).Str("resolved", resolved).AnErr("resolve_err", err).
		Msg("claude probe: binary lookup")
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%s. Install the Claude Code CLI on this machine, or set CLAUDE_CODE_BIN to its path: %w",
			err.Error(), domain.ErrAgentCLIBinaryMissing)
	}

	version, out, err := probeVersion(ctx, resolved)
	log.Info().Str("version", version).AnErr("version_err", err).Str("output", tail(out)).
		Msg("claude probe: --version")
	if err != nil {
		// Resolved and would not run. Same fix as "not installed" — repair the
		// install — so it carries the same sentinel, with the binary's own
		// complaint attached rather than replaced.
		return ProbeResult{}, fmt.Errorf("%s was found but would not run (%v)%s: %w",
			resolved, err, tail(out), domain.ErrAgentCLIBinaryMissing)
	}

	if err := probeAuth(ctx, resolved, settingSources); err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{BinaryPath: resolved, Version: version}, nil
}

// probeVersion runs `--version` and returns the first line of what it printed.
func probeVersion(ctx context.Context, bin string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeVersionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = childEnv(ctx)
	// A temp dir, not the server's working directory: --version reads nothing,
	// but every `claude` invocation resolves project settings from its cwd, and
	// the server's cwd is not a project anyone chose.
	cmd.Dir = os.TempDir()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		return "", out, err
	}
	line := strings.TrimSpace(out)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	return line, out, nil
}

// probeAuth runs one real session and turns its failure into the right sentinel.
func probeAuth(ctx context.Context, bin string, settingSources string) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultProbeTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "tt-cli-probe-")
	if err != nil {
		return fmt.Errorf("probe workspace could not be created: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	args := []string{
		"-p", probePrompt,
		"--output-format", "json",
		"--max-turns", "1",
		// The same settings sources a board run gets, so a host whose auth
		// lives in a user-level apiKeyHelper is probed exactly as it will be
		// run. Probing under different settings than the executor uses is how a
		// connect passes and every run afterwards fails.
		"--setting-sources", normalizeSettingSources(settingSources),
		// Nothing here is expected to touch the filesystem, but there is no
		// human at this terminal either: without it a session that decides to
		// look around blocks on a permission prompt until the timeout, and the
		// operator is told their CLI is broken when it was waiting for them.
		"--dangerously-skip-permissions",
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = childEnv(ctx)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	authStarted := time.Now()
	log.Info().Str("dir", dir).Str("setting_sources", normalizeSettingSources(settingSources)).
		Msg("claude probe: starting one-turn auth session")
	runErr := cmd.Run()
	out := buf.String()
	// The CLI's own words are the only diagnosis there is: an exit code alone
	// cannot tell a missing login from a crash from a timeout.
	log.Info().AnErr("run_err", runErr).Bool("timed_out", errors.Is(ctx.Err(), context.DeadlineExceeded)).
		Dur("took", time.Since(authStarted)).Str("output", tail(out)).
		Msg("claude probe: auth session finished")
	if runErr == nil {
		return nil
	}
	if looksUnauthenticated(out) {
		return fmt.Errorf("%s is installed but has no usable Claude session%s. Sign the CLI in on this machine (`claude` then /login, or set CLAUDE_CODE_OAUTH_TOKEN): %w",
			bin, tail(out), domain.ErrAgentCLIUnauthenticated)
	}
	// Neither of the two named failures. Report it as itself: the output is the
	// only thing that names what actually happened, and mapping it onto the
	// nearest sentinel would send the operator to fix something that is fine.
	return fmt.Errorf("%s did not complete a test session (%v)%s", bin, runErr, tail(out))
}

// looksUnauthenticated reports whether the CLI's output says the session had no
// credentials.
func looksUnauthenticated(out string) bool {
	lower := strings.ToLower(out)
	for _, marker := range authFailureMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// probeOutputMax is how much of the CLI's output travels in an error message.
// The message reaches a toast in a browser, and a CLI that fails during startup
// can print a stack trace longer than the page.
const probeOutputMax = 600

// tail renders the last of the CLI's output for a message, or nothing when it
// said nothing. The tail rather than the head, for the same reason the executor
// keeps the tail of stderr: a program that dies says why on its last lines.
func tail(out string) string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > probeOutputMax {
		trimmed = "…" + trimmed[len(trimmed)-probeOutputMax:]
	}
	return ": " + strings.Join(strings.Fields(trimmed), " ")
}
