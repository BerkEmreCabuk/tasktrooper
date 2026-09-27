package prompt

type shellBlockingCommandInput struct {
	Segment string
	What    string
}

var shellBlockingCommandKey = Define("guard.shell_blocking_command", shellBlockingCommandInput{Segment: "npm run dev", What: "a dev server"})

// ShellBlockingCommandText is run_terminal's refusal for a command whose only
// purpose is to run until interrupted — see adapter/tools/shell/blocking.go.
func ShellBlockingCommandText(segment, what string) string {
	return shellBlockingCommandKey.Render(shellBlockingCommandInput{Segment: segment, What: what})
}
