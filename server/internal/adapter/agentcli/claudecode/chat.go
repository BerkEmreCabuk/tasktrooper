package claudecode

import (
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ExecuteChat runs one chat turn in a CLI session.
//
// The board's Execute and this differ in exactly two ways, and both are
// consequences of a chat being a conversation rather than a job:
//
//   - Continuity. The first turn flattens the assembled history the same way a
//     board run does; every turn after it resumes the SAME CLI session and sends
//     only what the user just typed. Re-flattening a growing transcript every
//     time would pay for the whole conversation on each message, and would also
//     hand the model its own remembered context back as if it were new
//     instructions.
//   - Streaming. The answer is watched as it appears, so assistant text is
//     forwarded to out while the session is still running rather than returned
//     at the end.
//
// Everything else — the per-turn MCP credential, the deadline, the quota
// mapping — is the board path's, unchanged and shared.
//
// Two things Execute does are deliberately absent here: the account-wide quota
// gate and the concurrency slot. A chat turn has a person watching who typed
// it a second ago; queuing them behind board runs, or bouncing them off a gate
// a board task armed, would make an interactive reply wait on work nobody at
// the keyboard asked for. The person sees the usage-limit notice as soon as
// this turn's own session reports it, same as always — and a turn that
// succeeds still clears the gate for everyone else, in finish.
func (e *Executor) ExecuteChat(ctx context.Context, req domain.ChatExecution, out port.ChatStream) (domain.ChatResult, error) {
	if e == nil {
		return domain.ChatResult{}, errors.New("claude code executor is not configured")
	}
	// Same rule as a board run, for a different reason: a chat's tools are the
	// CLI's own file and shell tools, and starting one in an unspecified
	// directory points them at whatever this server's process happens to be
	// sitting in.
	if strings.TrimSpace(req.WorkDir) == "" {
		return domain.ChatResult{}, errors.New("claude code executor: no chat workspace to run in")
	}

	// One credential per TURN, minted when it starts and revoked when the turn
	// ends — not one per conversation. The thread may stay open for days; a
	// token that lived that long would authorise the board tools during every
	// minute nobody was talking.
	mcpCfg, releaseMCP, err := e.resolveMCP(ctx, MCPRun{Policy: req.Policy, Label: req.SessionID})
	defer releaseMCP()
	if err != nil {
		return domain.ChatResult{}, err
	}

	mcpPath, cleanupMCP, err := writeMCPConfigFile(mcpCfg)
	if err != nil {
		return domain.ChatResult{}, err
	}
	defer cleanupMCP()

	fresh := func() invocation {
		systemPrompt, prompt := flattenHistory(req.History)
		return invocation{
			workDir: req.WorkDir,
			// The chat turn needs the manifest for the same reason a board run
			// does: an agent asked in conversation to tick a criterion has to
			// call the tool by the name the session knows it under.
			systemPrompt: withToolManifest(systemPrompt, mcpCfg.Tools),
			prompt:       prompt,
			model:        req.Model,
			label:        req.SessionID,
			mcpPath:      mcpPath,
			stream:       out,
		}
	}

	inv := fresh()
	resumed := strings.TrimSpace(req.ResumeSessionID) != ""
	if resumed {
		// The live session already holds the persona, the workspace and
		// everything said so far. It needs the new message and nothing else —
		// no --append-system-prompt, no replayed transcript.
		inv.systemPrompt = ""
		inv.prompt = strings.TrimSpace(req.Prompt)
		inv.resumeSessionID = req.ResumeSessionID
		if inv.prompt == "" {
			// Nothing new to say and a session that already has the context: the
			// only honest thing left is the flattened history, which at least
			// carries the message the caller failed to isolate.
			_, inv.prompt = flattenHistory(req.History)
		}
	}

	s, err := e.spawn(ctx, inv)
	if err != nil {
		return domain.ChatResult{}, err
	}

	// A resume the CLI cannot honour. The session lives in the CLI's own
	// storage on this host, which this server neither owns nor can inspect: it
	// is pruned on its own schedule, lost with a reinstall, and simply absent if
	// the last turn ran on a different machine. None of that is a chat that
	// should stop working — the whole transcript is still in Postgres — so the
	// turn starts a fresh session with the flattened history and says so in the
	// log.
	if resumed && resumeRefused(s) {
		log.Info().
			Str("session_id", req.SessionID).
			Str("cli_session_id", req.ResumeSessionID).
			Msg("claude code could not resume this chat's cli session; starting a fresh one from the stored history")
		// Whatever the refused attempt put on the wire is not part of the
		// answer. In practice it puts nothing (the CLI gives up before its first
		// turn), so this is belt and braces — but a partial line left in front
		// of the real reply would be persisted as the agent's own words.
		out.SegmentBreak()
		retry := fresh()
		// One trace across both spawns, so the fresh session's turns are numbered
		// after the refused attempt's rather than on top of them. In practice the
		// refused attempt records nothing at all — the CLI gives up before its
		// first turn — which is exactly why this is cheap to get right.
		retry.trace = s.trace
		if s, err = e.spawn(ctx, retry); err != nil {
			return domain.ChatResult{}, err
		}
		resumed = false
	}

	resp, err := e.finish(ctx, req.SessionID, s)
	result := domain.ChatResult{
		Response: resp,
		// Reported even on the error paths. A turn that hit the usage limit
		// still leaves a live session behind, and resuming it when the quota
		// comes back is the difference between continuing the conversation and
		// starting it again.
		CLISessionID: s.sessionID(),
		Resumed:      resumed,
	}
	return result, err
}

// resumeRefused reports the CLI saying it has no such session.
//
// Narrow by construction: it fires only on a session that FAILED, and only on
// the CLI's own wording for a missing conversation. A broad match here would be
// expensive in the other direction — silently discarding a live session and
// restarting the conversation from scratch, at full context cost, because some
// unrelated error happened to contain the word "session".
func resumeRefused(s session) bool {
	if !s.failed() {
		return false
	}
	return resumeMissingPattern.MatchString(strings.Join([]string{s.out.Text, s.stderrTail}, "\n"))
}
