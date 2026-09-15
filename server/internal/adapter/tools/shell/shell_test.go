package shell_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	"github.com/makifbaysal/tasktrooper/server/internal/application/config"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/resources"
)

func offSandbox() domain.TerminalSandboxConfig {
	return domain.TerminalSandboxConfig{Mode: "off"}
}

// quote renders a command as a JSON string so test cases can contain newlines
// and quotes without hand-escaping them.
func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type ShellToolSuite struct {
	suite.Suite
}

func (s *ShellToolSuite) TestName() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	s.Equal("run_terminal", tool.Name())
}

func (s *ShellToolSuite) TestDefinition() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	def := tool.Definition()
	s.Equal("function", def.Type)
	s.Equal("run_terminal", def.Function.Name)
}

func (s *ShellToolSuite) TestExecuteEcho() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	result := tool.Execute(context.Background(), `{"command":"echo hello"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "hello")
}

// A non-zero exit is a failure. Reporting it as a success made every failing
// command invisible to the agent loop's progress guards — the error streak that
// ends a run where nothing lands never advanced, and the run's stats counted
// the failure as a completed call.
func (s *ShellToolSuite) TestExecuteNonZeroExit() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	result := tool.Execute(context.Background(), `{"command":"exit 1"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "exit")
}

// A command that succeeds without printing anything used to hand the agent an
// empty tool result, which reads as "the call produced nothing" — so the agent
// re-ran the same sed/mv/mkdir until the loop guard killed the run. Silence is
// how those commands report success and the result has to say so.
func (s *ShellToolSuite) TestExecuteSilentSuccessSaysItSucceeded() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	result := tool.Execute(context.Background(), `{"command":"true"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "exit status 0")
	s.Contains(result.Content, "no output")
	s.Contains(result.Content, "Do not re-run")
}

// The note is only for silent commands; a command that printed something must
// hand back its output untouched.
func (s *ShellToolSuite) TestExecuteWithOutputIsNotAnnotated() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	result := tool.Execute(context.Background(), `{"command":"echo hello"}`)
	s.Equal("hello\n", result.Content)
}

func (s *ShellToolSuite) TestExecuteInvalidArguments() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	result := tool.Execute(context.Background(), `not json`)
	s.True(result.IsError)
}

func (s *ShellToolSuite) TestExecuteEmptyCommand() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	result := tool.Execute(context.Background(), `{"command":""}`)
	s.True(result.IsError)
}

func (s *ShellToolSuite) TestExecuteWithWorkingDir() {
	tool := shell.New("/tmp", 10*time.Second, 0, offSandbox())
	result := tool.Execute(context.Background(), `{"command":"pwd","working_dir":"/var/tmp"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "var")
}

func (s *ShellToolSuite) TestExecuteTimeout() {
	tool := shell.New("/tmp", 100*time.Millisecond, 0, offSandbox())
	result := tool.Execute(context.Background(), `{"command":"sleep 5"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "timed out")
}

func (s *ShellToolSuite) TestSandboxAllowlistBlocks() {
	sandbox := domain.TerminalSandboxConfig{
		Mode:            "allowlist",
		AllowedCommands: []string{"echo"},
	}
	tool := shell.New("/tmp", 10*time.Second, 0, sandbox)
	result := tool.Execute(context.Background(), `{"command":"rm -rf /"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "allowlist")
}

// The allowlist used to check only the first word, so any allowlisted command
// could carry a payload behind a separator.
func (s *ShellToolSuite) TestSandboxAllowlistBlocksChainedCommands() {
	sandbox := domain.TerminalSandboxConfig{
		Mode:            "allowlist",
		AllowedCommands: []string{"echo"},
	}
	tool := shell.New("/tmp", 10*time.Second, 0, sandbox)

	for _, command := range []string{
		`echo hi; rm -rf /`,
		`echo hi && rm -rf /`,
		`echo hi || rm -rf /`,
		`echo hi | sh`,
		"echo hi\nrm -rf /",
		`echo hi & rm -rf /`,
	} {
		result := tool.Execute(context.Background(), `{"command":`+quote(command)+`}`)
		s.True(result.IsError, "expected %q to be rejected", command)
		s.Contains(result.Content, "allowlist")
	}
}

// A substitution hides a command inside an argument, where splitting on
// separators cannot find it.
func (s *ShellToolSuite) TestSandboxAllowlistBlocksCommandSubstitution() {
	sandbox := domain.TerminalSandboxConfig{
		Mode:            "allowlist",
		AllowedCommands: []string{"echo"},
	}
	tool := shell.New("/tmp", 10*time.Second, 0, sandbox)

	for _, command := range []string{"echo $(rm -rf /)", "echo `rm -rf /`"} {
		result := tool.Execute(context.Background(), `{"command":`+quote(command)+`}`)
		s.True(result.IsError, "expected %q to be rejected", command)
		s.Contains(result.Content, "substitution")
	}
}

// Chaining stays usable as long as every command in the line is allowlisted,
// and a leading env assignment is judged on the command it prefixes.
func (s *ShellToolSuite) TestSandboxAllowlistAllowsFullyAllowedChain() {
	sandbox := domain.TerminalSandboxConfig{
		Mode:            "allowlist",
		AllowedCommands: []string{"echo", "true"},
	}
	tool := shell.New("/tmp", 10*time.Second, 0, sandbox)

	result := tool.Execute(context.Background(), `{"command":"echo hi && true"}`)
	s.False(result.IsError, result.Content)

	result = tool.Execute(context.Background(), `{"command":"FOO=bar echo hi"}`)
	s.False(result.IsError, result.Content)
}

func (s *ShellToolSuite) TestSandboxBlocklist() {
	sandbox := domain.TerminalSandboxConfig{
		Mode:            "blocklist",
		BlockedPatterns: []string{"rm -rf"},
	}
	tool := shell.New("/tmp", 10*time.Second, 0, sandbox)
	result := tool.Execute(context.Background(), `{"command":"rm -rf /tmp"}`)
	s.True(result.IsError)
	s.Contains(result.Content, "blocked")
}

// Blocklist mode used to return the moment no pattern matched, which skipped
// the working-directory check below it: turning the sandbox from allowlist to
// blocklist silently also turned off directory scoping.
func (s *ShellToolSuite) TestSandboxBlocklistStillRestrictsWorkingDir() {
	base := s.T().TempDir()
	outside := s.T().TempDir()
	sandbox := domain.TerminalSandboxConfig{
		Mode:               "blocklist",
		BlockedPatterns:    []string{"rm -rf"},
		RestrictWorkingDir: true,
	}
	tool := shell.New(base, 10*time.Second, 0, sandbox)

	result := tool.Execute(context.Background(), `{"command":"pwd","working_dir":`+quote(outside)+`}`)

	s.True(result.IsError, result.Content)
	s.Contains(result.Content, "outside")
}

// The sandbox every install runs. A developer agent's loop is edit → build
// → verify, and the shipped allowlist held no command that can write a file:
// `cat`/`head` read fine, every edit came back "not in allowlist", and the
// agent reported to the board that it lacked permission to edit files.
func (s *ShellToolSuite) TestShippedSandboxLetsAnAgentEditAndBuild() {
	cfg, err := config.Parse(resources.ConfigYAML)
	s.Require().NoError(err)

	dir := s.T().TempDir()
	tool := shell.New(dir, 10*time.Second, 0, cfg.Tools.Terminal.Sandbox)
	ctx := registry.ContextWithWorkspaceDir(context.Background(), dir)

	// A TypeScript file carrying a template literal: backticks are ordinary
	// source, and allowlist mode rejected the whole command for containing one.
	write := tool.Execute(ctx, `{"command":`+quote("printf 'const a = `x`;\n' > app.ts")+`}`)
	s.False(write.IsError, write.Content)

	edit := tool.Execute(ctx, `{"command":`+quote("sed 's/x/y/' app.ts > out.ts && cat out.ts")+`}`)
	s.False(edit.IsError, edit.Content)
	s.Contains(edit.Content, "y")

	// npm is how the frontend agent's own rule tells it to verify a change.
	build := tool.Execute(ctx, `{"command":"npm --version"}`)
	s.NotContains(build.Content, "not in allowlist")
}

func (s *ShellToolSuite) TestExecuteRepositoryScopedWorkingDir() {
	root := s.T().TempDir()
	sandbox := domain.TerminalSandboxConfig{
		Mode:               "allowlist",
		AllowedCommands:    []string{"pwd"},
		RestrictWorkingDir: true,
	}
	tool := shell.New("/var/not-used", 10*time.Second, 0, sandbox)
	ctx := registry.ContextWithWorkspaceDir(context.Background(), root)
	result := tool.Execute(ctx, `{"command":"pwd"}`)
	s.False(result.IsError)
	s.Contains(result.Content, root)
}

// Paging a file with a ten-line `sed -n` window costs a whole agent turn per
// window: a production run spent sixty of its eighty iterations sliding one
// down a 1200-line file and died out of budget before making its edit. Neither
// loop guard can see it — every window has different arguments, so nothing
// repeats and nothing fails — so the result itself has to name the way out.
func (s *ShellToolSuite) TestNarrowSedWindowPointsAtReadFile() {
	root := s.T().TempDir()
	tool := shell.New(root, 10*time.Second, 0, offSandbox())

	seed := tool.Execute(context.Background(), `{"command":`+quote("printf 'a\nb\nc\nd\n' > f.txt")+`}`)
	s.False(seed.IsError, seed.Content)

	result := tool.Execute(context.Background(), `{"command":`+quote("sed -n '2,4p' f.txt")+`}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "b")
	s.Contains(result.Content, "read_file")
}

// A wide range is someone reading a whole file, not scrolling through it; the
// hint would be noise on every legitimate call.
func (s *ShellToolSuite) TestWideSedRangeIsNotNudged() {
	root := s.T().TempDir()
	tool := shell.New(root, 10*time.Second, 0, offSandbox())

	seed := tool.Execute(context.Background(), `{"command":`+quote("printf 'a\nb\n' > f.txt")+`}`)
	s.False(seed.IsError, seed.Content)

	result := tool.Execute(context.Background(), `{"command":`+quote("sed -n '1,900p' f.txt")+`}`)

	s.False(result.IsError, result.Content)
	s.NotContains(result.Content, "read_file")
}

func TestShellToolSuite(t *testing.T) {
	suite.Run(t, new(ShellToolSuite))
}
