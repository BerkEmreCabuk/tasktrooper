package config_test

import (
	"testing"
	"time"

	appconfig "github.com/makifbaysal/tasktrooper/server/internal/application/config"
	"github.com/stretchr/testify/require"
)

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
