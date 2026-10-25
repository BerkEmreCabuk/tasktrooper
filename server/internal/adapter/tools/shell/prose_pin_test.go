package shell

import (
	"context"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestBlockingCommandReasonMessageUnchanged pins the exact refusal, ahead of
// moving it into catalog/system/guards/shell_blocking_command.md.
func TestBlockingCommandReasonMessageUnchanged(t *testing.T) {
	got := blockingCommandReason("npm run dev")
	want := "refused: `npm run dev` starts a dev server, which runs until it is interrupted. " +
		"Run in the foreground it cannot finish — it would hold this tool until the timeout and return nothing but a partial log. " +
		"To check that the code works, run the build, typecheck or test command instead. " +
		"If you genuinely need the process up, start it detached and read its log from your task's own scratch directory, never the fixed `/tmp/dev.log` (concurrent agents collide there): " +
		"`mkdir -p /tmp/tt-<task key> && npm run dev > /tmp/tt-<task key>/dev.log 2>&1 &` then `sleep 5; cat /tmp/tt-<task key>/dev.log`."
	if got != want {
		t.Errorf("blockingCommandReason(\"npm run dev\") = %q, want %q", got, want)
	}
}

// TestRunTerminalProseUnchanged pins run_terminal's remaining instructive
// result text — timeout retry hints, the "silence is success" note and the
// sliding-window nudge — ahead of moving it into
// catalog/system/guards and catalog/system/prompts/tool_results.
func TestRunTerminalProseUnchanged(t *testing.T) {
	sandbox := domain.TerminalSandboxConfig{}

	t.Run("timeout below the ceiling suggests raising it", func(t *testing.T) {
		tool := New(t.TempDir(), 1*time.Second, 10*time.Second, sandbox)
		res := tool.Execute(context.Background(), `{"command":"sleep 5"}`)
		if !res.IsError {
			t.Fatalf("expected a timeout error, got: %s", res.Content)
		}
		want := "command timed out after 1s. This command needs longer than 1s: re-run it with timeout_seconds up to 10. " +
			"Do not repeat it unchanged — it will hit the same wall.\npartial output:\n"
		if res.Content != want {
			t.Errorf("= %q, want %q", res.Content, want)
		}
	})

	t.Run("timeout at the ceiling says narrow the command", func(t *testing.T) {
		tool := New(t.TempDir(), 1*time.Second, 1*time.Second, sandbox)
		res := tool.Execute(context.Background(), `{"command":"sleep 5"}`)
		if !res.IsError {
			t.Fatalf("expected a timeout error, got: %s", res.Content)
		}
		want := "command timed out after 1s. This is already the maximum budget (1s). Narrow the command — build or test one package" +
			" instead of the whole tree — rather than repeating it.\npartial output:\n"
		if res.Content != want {
			t.Errorf("= %q, want %q", res.Content, want)
		}
	})

	t.Run("silent success says so", func(t *testing.T) {
		tool := New(t.TempDir(), 5*time.Second, 5*time.Second, sandbox)
		res := tool.Execute(context.Background(), `{"command":"true"}`)
		if res.IsError {
			t.Fatalf("unexpected error: %s", res.Content)
		}
		want := "exit status 0, no output. The command ran and succeeded; it simply printed nothing. " +
			"Writers (sed, mv, cp, mkdir) are silent on success, and a check that found nothing " +
			"(git status --porcelain on a clean tree) is silent too. Do not re-run it to check — read the " +
			"file or the state if you need to confirm."
		if res.Content != want {
			t.Errorf("= %q, want %q", res.Content, want)
		}
	})

	t.Run("sliding sed window is redirected to read_file", func(t *testing.T) {
		got := windowedReadHint(`sed -n '10,20p' file.go`)
		want := "\n\n[You read this file through a 11-line window. Use read_file instead: " +
			"it returns up to 800 numbered lines and the file's total length in a single call. " +
			"Sliding a small window down a file costs one whole agent turn per window.]"
		if got != want {
			t.Errorf("windowedReadHint() = %q, want %q", got, want)
		}
	})
}
