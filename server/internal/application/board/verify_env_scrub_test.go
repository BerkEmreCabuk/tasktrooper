package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// VerifyEnvScrubSuite covers the verify gate's own exec, which is a separate
// door into the same secrets as run_terminal.
//
// A verify stage is a command the repository under test declares — its
// verify_command, or its package.json "test" script. It used to run with
// append(os.Environ(), …), so a repository the agent was asked to work on could
// exfiltrate the pod's DATABASE_URL, INTERNAL_AUTH_KEY and MCP_SECRETS_KEY from
// its own test script without ever issuing a tool call. Scrubbing run_terminal
// routed around that door; these tests assert it is now shut.
type VerifyEnvScrubSuite struct {
	suite.Suite
}

func TestVerifyEnvScrubSuite(t *testing.T) {
	suite.Run(t, new(VerifyEnvScrubSuite))
}

// verifyPlantedSecrets mirrors the tenant Deployment's env
// (internal/control/kube) plus a provider key.
var verifyPlantedSecrets = map[string]string{
	"DATABASE_URL":      "postgres://tenant:hunter2@10.0.0.5:5432/tenant_x",
	"INTERNAL_AUTH_KEY": "gateway-hmac-key-9f21",
	"MCP_SECRETS_KEY":   "bWNwLXNlY3JldHMta2V5",
	"OPENAI_API_KEY":    "sk-proj-must-not-leak",
	"ANTHROPIC_API_KEY": "sk-ant-must-not-leak",
	"SERVER_API_KEY":    "bridge-server-key",
	"PG_PASSWORD":       "postgres-superuser-password",
}

func (s *VerifyEnvScrubSuite) plantSecrets() {
	for name, value := range verifyPlantedSecrets {
		s.T().Setenv(name, value)
	}
}

func (s *VerifyEnvScrubSuite) assertNoSecrets(output string) {
	for name, value := range verifyPlantedSecrets {
		s.NotContains(output, value, "secret value for %s reached the verify stage", name)
		s.NotContains(output, name+"=", "variable %s reached the verify stage", name)
	}
}

// hostileRepo writes a workspace whose declared verify command dumps its own
// environment and then fails, so runVerification hands the child's environment
// back in the failure report — the same channel the agent is fed for its fix
// round, and therefore the same channel an attacker would read.
func (s *VerifyEnvScrubSuite) hostileRepo(script string) (string, domain.Repository) {
	dir := s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "leak.sh"), []byte(script+"\nexit 1\n"), 0o700))
	return dir, domain.Repository{VerifyCommand: "sh leak.sh"}
}

func (s *VerifyEnvScrubSuite) TestRepoDeclaredStageGetsNoSecrets() {
	s.plantSecrets()
	dir, repo := s.hostileRepo("env")

	ok, report := runVerification(context.Background(), dir, repo)

	s.False(ok, "the stage exits 1, so verification must report a failure")
	s.assertNoSecrets(report)
}

// A repo's script is not limited to `env`, and the defense must not depend on
// which spelling it picks: none of these has anything to read.
func (s *VerifyEnvScrubSuite) TestObfuscatedReadsInAStageFindNothing() {
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
		dir, repo := s.hostileRepo(script)
		_, report := runVerification(context.Background(), dir, repo)
		s.assertNoSecrets(report)
	}
}

// The gate exists to judge the agent's diff with the toolchain the repo pins.
// A scrub that stripped the toolchain would make every verification fail for
// the wrong reason, and the first person to debug that would delete the scrub.
func (s *VerifyEnvScrubSuite) TestStageKeepsItsToolchain() {
	s.T().Setenv("GOFLAGS", "-mod=mod")
	s.T().Setenv("JAVA_HOME", "/opt/java")
	dir, repo := s.hostileRepo(`echo path=[$PATH] home=[$HOME] goflags=[$GOFLAGS] java=[$JAVA_HOME]`)

	_, report := runVerification(context.Background(), dir, repo)

	s.NotContains(report, "path=[]")
	s.NotContains(report, "home=[]")
	s.Contains(report, "goflags=[-mod=mod]")
	s.Contains(report, "java=[/opt/java]")
}

// The empty-overlay case is the one that regresses silently: cmd.Env used to be
// left nil when the repo declared no toolchain, and exec reads nil as "inherit
// the parent's environment" — a full leak that looks like no code at all.
func (s *VerifyEnvScrubSuite) TestNoToolchainOverlayStillScrubs() {
	s.plantSecrets()
	// An empty dir declares no Go/Node/Python version, so the toolchain resolver
	// produces an overlay with nothing in it.
	dir, repo := s.hostileRepo("env")

	ok, report := runVerification(context.Background(), dir, repo)

	s.False(ok)
	s.NotEmpty(report, "the stage's output must reach the report, or this proves nothing")
	s.assertNoSecrets(report)
}
