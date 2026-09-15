package mcp

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// StdioEnvScrubSuite covers the third door into the pod's secrets: a stdio MCP
// server subprocess.
//
// MCP server configs are user input — a command, its args and its environment,
// stored and re-resolved on every reload. The subprocess used to get
// the entire bridge environment merged with the config's overrides, so any
// configured server (an npx package, a script) ran with DATABASE_URL,
// INTERNAL_AUTH_KEY and MCP_SECRETS_KEY in reach. The scrub has to remove the
// ambient environment without removing the credentials the config supplies on
// purpose, which is what these tests separate.
type StdioEnvScrubSuite struct {
	suite.Suite
}

func TestStdioEnvScrubSuite(t *testing.T) {
	suite.Run(t, new(StdioEnvScrubSuite))
}

// mcpPlantedSecrets mirrors a parent process's env plus a provider key.
var mcpPlantedSecrets = map[string]string{
	"DATABASE_URL":      "postgres://tenant:hunter2@10.0.0.5:5432/tenant_x",
	"INTERNAL_AUTH_KEY": "gateway-hmac-key-9f21",
	"MCP_SECRETS_KEY":   "bWNwLXNlY3JldHMta2V5",
	"OPENAI_API_KEY":    "sk-proj-must-not-leak",
	"ANTHROPIC_API_KEY": "sk-ant-must-not-leak",
	"SERVER_API_KEY":    "bridge-server-key",
	"PG_PASSWORD":       "postgres-superuser-password",
}

func (s *StdioEnvScrubSuite) plantSecrets() {
	for name, value := range mcpPlantedSecrets {
		s.T().Setenv(name, value)
	}
}

func (s *StdioEnvScrubSuite) assertNoSecrets(output string) {
	for name, value := range mcpPlantedSecrets {
		s.NotContains(output, value, "secret value for %s reached the mcp subprocess", name)
		s.NotContains(output, name+"=", "variable %s reached the mcp subprocess", name)
	}
}

// runWithServerEnv launches a real subprocess with exactly the environment
// connectServer would give a stdio MCP server. Asserting on the slice alone
// would only prove what the function returns; running a process proves what a
// server can actually read, which is the property under test.
func (s *StdioEnvScrubSuite) runWithServerEnv(overrides map[string]string, script string) string {
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = stdioServerEnv(overrides)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	return out.String()
}

func (s *StdioEnvScrubSuite) TestServerSubprocessGetsNoAmbientSecrets() {
	s.plantSecrets()

	s.assertNoSecrets(s.runWithServerEnv(nil, "env"))
}

// Same argument as the shell tool's suite: if one spelling leaks and another
// does not, the defense has regressed into text matching. None of these has
// anything to find.
func (s *StdioEnvScrubSuite) TestObfuscatedReadsFindNothing() {
	s.plantSecrets()

	for _, script := range []string{
		`env`,
		`printenv`,
		`e''nv`,
		`export -p`,
		`set`,
		`echo "$INTERNAL_AUTH_KEY$DATABASE_URL$MCP_SECRETS_KEY"`,
		`printf '%s' "$(env)"`,
		`cat /proc/self/environ`,
	} {
		s.assertNoSecrets(s.runWithServerEnv(nil, script))
	}
}

// The part that makes this call site different from the other two: an MCP
// server config normally carries that server's own credentials, and those are
// configured rather than ambient. Dropping them would break every server that
// needs a token, which is most of them.
func (s *StdioEnvScrubSuite) TestConfiguredCredentialsStillReachTheServer() {
	s.plantSecrets()

	out := s.runWithServerEnv(map[string]string{
		"GITHUB_TOKEN":    "ghp-configured-for-this-server",
		"VENDOR_API_KEY":  "vendor-configured-for-this-server",
		"MCP_SERVER_MODE": "readonly",
	}, `echo token=[$GITHUB_TOKEN] vendor=[$VENDOR_API_KEY] mode=[$MCP_SERVER_MODE]`)

	s.Contains(out, "token=[ghp-configured-for-this-server]")
	s.Contains(out, "vendor=[vendor-configured-for-this-server]")
	s.Contains(out, "mode=[readonly]")
	// The ambient secrets are still gone: what the config asked for arrived,
	// what the pod happened to be holding did not.
	s.assertNoSecrets(out)
}

// An override of a forwarded variable must still win. The config is the more
// specific statement of intent, and the previous implementation (a map merge
// with overrides applied last) honoured it — that behaviour cannot regress.
func (s *StdioEnvScrubSuite) TestConfiguredOverrideBeatsTheInheritedValue() {
	s.T().Setenv("NODE_ENV", "production")

	out := s.runWithServerEnv(map[string]string{"NODE_ENV": "test"},
		`echo node_env=[$NODE_ENV]`)

	s.Contains(out, "node_env=[test]")
	s.NotContains(out, "node_env=[production]")
}

// A server that cannot resolve its own interpreter never starts, and a
// connect failure is indistinguishable from a broken config to whoever is
// looking at the health endpoint.
func (s *StdioEnvScrubSuite) TestServerKeepsWhatItNeedsToRun() {
	s.T().Setenv("NODE_PATH", "/opt/node/lib")

	out := s.runWithServerEnv(nil, `echo path=[$PATH] home=[$HOME] nodepath=[$NODE_PATH]`)

	s.NotContains(out, "path=[]")
	s.NotContains(out, "home=[]")
	s.Contains(out, "nodepath=[/opt/node/lib]")
}

// Overrides come from a map; without sorting, the same config produced a
// different argument order on every connect, which turns a reproducible
// server failure into an intermittent one.
func (s *StdioEnvScrubSuite) TestOverrideOrderIsDeterministic() {
	overrides := map[string]string{"B_VAR": "2", "A_VAR": "1", "C_VAR": "3"}

	first := stdioServerEnv(overrides)
	for range 20 {
		s.Equal(first, stdioServerEnv(overrides))
	}
	tail := first[len(first)-3:]
	s.Equal([]string{"A_VAR=1", "B_VAR=2", "C_VAR=3"}, tail,
		"configured entries must be sorted and appended last")
	s.True(strings.HasPrefix(tail[0], "A_VAR="))
}
