package runtime

import (
	"os"

	"github.com/rs/zerolog/log"
)

// processSecretVars are credentials the desktop passes in as plaintext env
// vars, read exactly once at boot; after that the entries are pure liability.
// Scrubbing the CHILD environment does not remove them from the PARENT, and
// exec sites outside the three hardened ones (internal/adapter/vcs/git among
// them) still build their child environment from os.Environ(). Unsetting closes
// all of them at once, including for exec sites added later that forget to
// scrub.
var processSecretVars = []string{
	// Consumed via Options.PostgresDSN and by the pool cmd/agent-server opens.
	"DATABASE_URL",
	// Consumed via Options.APIKey; the reload path re-applies Options over it.
	"SERVER_API_KEY",
	// Encrypts provider keys/secrets at rest; the engine derives the cipher
	// once before this runs and hands it to every later caller.
	"MCP_SECRETS_KEY",
}

// scrubProcessSecrets removes the injected credentials from the process
// environment once their values are held in memory.
//
// It cannot touch Linux /proc/<pid>/environ, the exec-time stack region that
// os.Unsetenv has no unprivileged way to rewrite; denyProcEnvironReads closes
// that door separately. What it does close is every os.Environ()-based exec
// site plus ${VAR} expansion in user-supplied MCP configs, which could
// otherwise ask for ${DATABASE_URL} by name. Failures are logged and ignored:
// os.Unsetenv only fails on a malformed name, and refusing to boot over it
// would trade a leak for an outage.
func scrubProcessSecrets() {
	for _, name := range processSecretVars {
		if _, present := os.LookupEnv(name); !present {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			log.Warn().Err(err).Str("var", name).Msg("could not unset secret from process environment")
			continue
		}
		log.Debug().Str("var", name).Msg("secret removed from process environment")
	}
}
