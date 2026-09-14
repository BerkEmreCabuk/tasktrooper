package config_test

import (
	"testing"
	"time"

	appconfig "github.com/makifbaysal/tasktrooper/server/internal/application/config"
	"github.com/stretchr/testify/require"
)

// The shipped default was 60s, which is below the cost of the loop every
// developer agent runs — install, build, test — so the agent could never verify
// its own work. This pins the shipped budgets rather than the code defaults:
// the bug was in the config file, not in the fallback.
func TestShippedTerminalBudgetsFitARealBuild(t *testing.T) {
	cfg, err := appconfig.Load("../../../resources/config.yml")
	require.NoError(t, err)

	require.GreaterOrEqual(t, cfg.Tools.Terminal.Timeout, 2*time.Minute,
		"the default budget must outlast an ordinary build")
	require.GreaterOrEqual(t, cfg.Tools.Terminal.MaxTimeout, 10*time.Minute,
		"the ceiling must cover a cold dependency install plus a build")
	require.GreaterOrEqual(t, cfg.Tools.Terminal.MaxTimeout, cfg.Tools.Terminal.Timeout,
		"a ceiling under the default would silently shorten every command")
}
