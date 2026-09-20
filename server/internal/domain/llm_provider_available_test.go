package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// cursor_agent now has both halves built: application/agentfs.FlavorCursor
// renders the catalog and internal/adapter/cli/cursor executes it, so the
// definition must say it is available like any other host-executed CLI.
func TestCursorAgentIsDeclaredAsAnAvailableHostExecutedProvider(t *testing.T) {
	def, ok := domain.LLMProviderDefinitionFor(domain.LLMProviderCursorAgent)

	require.True(t, ok, "the provider must be listed, or the UI has nothing to show")
	require.Equal(t, domain.LLMProviderCursorAgent, def.Type)
	require.True(t, def.HostExecuted, "it is a process on the runner host, not an endpoint")
	require.True(t, def.Available, "internal/adapter/cli/cursor hands a task to cursor-agent")
	require.False(t, def.RequiresAPIKey, "a CLI carries its own subscription auth")
	require.False(t, def.BaseURLRequired, "there is no endpoint to dial")
	require.False(t, def.ModelRequired)
	require.Empty(t, def.DefaultBaseURL)

	require.True(t, domain.ValidLLMProviderType("cursor_agent"))
	require.True(t, domain.ProviderAvailable(domain.LLMProviderCursorAgent))
}

// antigravity follows the exact same pattern: internal/adapter/cli/antigravity
// is now the executor and application/agentfs.FlavorAntigravity is the catalog.
func TestAntigravityIsDeclaredAsAnAvailableHostExecutedProvider(t *testing.T) {
	def, ok := domain.LLMProviderDefinitionFor(domain.LLMProviderAntigravity)

	require.True(t, ok, "the provider must be listed, or the UI has nothing to show")
	require.Equal(t, domain.LLMProviderAntigravity, def.Type)
	require.True(t, def.HostExecuted, "it is a process on the runner host, not an endpoint")
	require.True(t, def.Available, "internal/adapter/cli/antigravity hands a task to agy")
	require.False(t, def.RequiresAPIKey, "a CLI carries its own subscription auth")
	require.False(t, def.BaseURLRequired, "there is no endpoint to dial")
	require.False(t, def.ModelRequired)
	require.Empty(t, def.DefaultBaseURL)

	require.True(t, domain.ValidLLMProviderType("antigravity"))
	require.True(t, domain.ProviderAvailable(domain.LLMProviderAntigravity))
}

// opencode is the newest of the local-CLI providers: internal/adapter/cli/opencode
// is its executor, and it has no dedicated agentfs flavor beyond the shared
// .claude/skills directory (see agentfs/opencode.go).
func TestOpencodeIsDeclaredAsAnAvailableHostExecutedProvider(t *testing.T) {
	def, ok := domain.LLMProviderDefinitionFor(domain.LLMProviderOpencode)

	require.True(t, ok, "the provider must be listed, or the UI has nothing to show")
	require.Equal(t, domain.LLMProviderOpencode, def.Type)
	require.True(t, def.HostExecuted, "it is a process on the runner host, not an endpoint")
	require.True(t, def.Available, "internal/adapter/cli/opencode hands a task to opencode")
	require.False(t, def.RequiresAPIKey, "a CLI carries its own provider auth")
	require.False(t, def.BaseURLRequired, "there is no endpoint to dial")
	require.False(t, def.ModelRequired)
	require.Empty(t, def.DefaultBaseURL)

	require.True(t, domain.ValidLLMProviderType("opencode"))
	require.True(t, domain.ProviderAvailable(domain.LLMProviderOpencode))
}

// Every provider is available today. The assertion is written this way round
// on purpose: Available is set explicitly on each definition rather than
// defaulted, so a provider added without a considered value fails closed — and
// this test is where that shows up, at the moment it is added, instead of as a
// provider that silently refuses everything in production.
func TestEveryProviderExceptDeclaredButUnbuiltIsAvailable(t *testing.T) {
	unavailable := []domain.LLMProviderType{}
	for _, def := range domain.AllLLMProviderDefinitions() {
		if !def.Available {
			unavailable = append(unavailable, def.Type)
		}
	}
	require.Empty(t, unavailable, "every declared provider now has an executor")

	require.True(t, domain.ProviderAvailable(domain.LLMProviderClaudeCode),
		"claude_code has an executor and must stay selectable on an agent")
	require.True(t, domain.ProviderAvailable(domain.LLMProviderAnthropic))
}

// An unknown type is not available either. Same answer ValidLLMProviderType
// gives it, so a typo cannot be mistaken for a ready provider.
func TestAnUnknownProviderIsNotAvailable(t *testing.T) {
	require.False(t, domain.ProviderAvailable(domain.LLMProviderType("cursor")))
	require.False(t, domain.ProviderAvailable(""))
}

// The refusal has to name the provider and the reason, and carry the sentinel:
// the sentinel is what lets the transport answer 400 and the retry loops stop,
// because nothing about a missing executor becomes true on the second attempt.
func TestUnavailableProviderErrorNamesTheProviderAndIsPermanent(t *testing.T) {
	err := domain.ErrUnavailableProvider(domain.LLMProviderCursorAgent)

	require.ErrorIs(t, err, domain.ErrProviderUnavailable)
	require.Contains(t, err.Error(), "Cursor")
	require.Contains(t, err.Error(), "not available yet")
}
