package shell

import "testing"

// TestBlockingCommandReasonMessageUnchanged pins the exact refusal, ahead of
// moving it into catalog/system/guards/shell_blocking_command.md.
func TestBlockingCommandReasonMessageUnchanged(t *testing.T) {
	got := blockingCommandReason("npm run dev")
	want := "refused: `npm run dev` starts a dev server, which runs until it is interrupted. " +
		"Run in the foreground it cannot finish — it would hold this tool until the timeout and return nothing but a partial log. " +
		"To check that the code works, run the build, typecheck or test command instead. " +
		"If you genuinely need the process up, start it detached and read its log: " +
		"`npm run dev > /tmp/dev.log 2>&1 &` then `sleep 5; cat /tmp/dev.log`."
	if got != want {
		t.Errorf("blockingCommandReason(\"npm run dev\") = %q, want %q", got, want)
	}
}
