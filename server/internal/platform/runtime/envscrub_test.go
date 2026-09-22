package runtime

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
)

// EnvScrubSuite covers the PARENT side of the leak: scrubbing what a child
// inherits does nothing about this process's own environment, which the exec
// sites outside the hardened three still copy from os.Environ().
type EnvScrubSuite struct {
	suite.Suite
}

func TestEnvScrubSuite(t *testing.T) {
	suite.Run(t, new(EnvScrubSuite))
}

func (s *EnvScrubSuite) TestInjectedSecretsAreRemovedFromTheProcess() {
	// The exact values the desktop app passes to this process.
	s.T().Setenv("DATABASE_URL", "postgres://tenant:hunter2@10.0.0.5:5432/tenant_x")
	s.T().Setenv("SERVER_API_KEY", "bearer-token-9f21")
	s.T().Setenv("MCP_SECRETS_KEY", "bWNwLXNlY3JldHMta2V5")

	scrubProcessSecrets()

	for _, name := range []string{"DATABASE_URL", "SERVER_API_KEY", "MCP_SECRETS_KEY"} {
		_, present := os.LookupEnv(name)
		s.False(present, "%s must not survive the scrub", name)
	}
	for _, entry := range os.Environ() {
		s.NotContains(entry, "hunter2")
		s.NotContains(entry, "bearer-token-9f21")
		s.NotContains(entry, "bWNwLXNlY3JldHMta2V5")
	}
}

// The scrub runs on every boot; unsetting an absent variable is a no-op.
func (s *EnvScrubSuite) TestAbsentSecretsAreNotAnError() {
	for _, name := range processSecretVars {
		s.T().Setenv(name, "placeholder")
		s.Require().NoError(os.Unsetenv(name))
	}

	s.NotPanics(scrubProcessSecrets)
}

// The blast radius of this list is the whole product, so each name was traced
// to its last reader before being added, and this test pins what is NOT scrubbed.
func (s *EnvScrubSuite) TestScrubListStaysMinimal() {
	s.ElementsMatch(
		[]string{"DATABASE_URL", "SERVER_API_KEY", "MCP_SECRETS_KEY"},
		processSecretVars,
	)

	// Left in place on purpose:
	//   POSTGRES_DSN    — config.yml expands ${POSTGRES_DSN} again on every
	//                     reload; DATABASE_URL is the name THIS process reads.
	//   ANTHROPIC/OPENAI/GOOGLE_API_KEY — read at boot, and user MCP configs may
	//                     legitimately reference them by ${VAR} on reload.
	for _, name := range processSecretVars {
		s.NotEqual("POSTGRES_DSN", name)
		s.NotEqual("ANTHROPIC_API_KEY", name)
		s.NotEqual("OPENAI_API_KEY", name)
		s.NotEqual("GOOGLE_API_KEY", name)
	}
}

// Non-secret configuration shares the environment and must come through.
func (s *EnvScrubSuite) TestNonSecretConfigurationSurvives() {
	s.T().Setenv("DATABASE_URL", "postgres://tenant:hunter2@10.0.0.5:5432/tenant_x")
	s.T().Setenv("SHUTDOWN_GRACE", "9m")
	s.T().Setenv("DATA_DIR", "/data")
	s.T().Setenv("PORT", "8080")

	scrubProcessSecrets()

	s.Equal("9m", os.Getenv("SHUTDOWN_GRACE"))
	s.Equal("/data", os.Getenv("DATA_DIR"))
	s.Equal("8080", os.Getenv("PORT"))
	s.Empty(os.Getenv("DATABASE_URL"))
}
