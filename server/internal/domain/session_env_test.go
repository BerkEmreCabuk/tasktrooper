package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestSessionEnvPassesPinsAndRefusesWhatCouldChooseAProgram(t *testing.T) {
	allowed, refused := domain.SessionEnv(map[string]string{
		"NODE_VERSION":   "20.11.0",
		"GOTOOLCHAIN":    "go1.24.0",
		"TT_TASK_KEY":    "t-12",
		"PATH":           "/tmp/evil",
		"NODE_OPTIONS":   "--require /tmp/x.js",
		"TT_":            "bare prefix",
		"TT_BAD-NAME":    "x",
		"PYTHON_VERSION": "3.12\x001",
		"RUBY_VERSION":   strings.Repeat("9", 5000),
	})

	assert.Equal(t, []string{"GOTOOLCHAIN=go1.24.0", "NODE_VERSION=20.11.0", "TT_TASK_KEY=t-12"}, allowed)
	assert.ElementsMatch(t, []string{"PATH", "NODE_OPTIONS", "TT_", "TT_BAD-NAME", "PYTHON_VERSION", "RUBY_VERSION"}, refused)
}

func TestSessionEnvOfNothingIsNothing(t *testing.T) {
	allowed, refused := domain.SessionEnv(nil)
	assert.Empty(t, allowed)
	assert.Empty(t, refused)
}
