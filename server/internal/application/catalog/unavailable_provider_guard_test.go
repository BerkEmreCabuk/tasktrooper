package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// This file used to hold three integration tests proving that selecting a
// provider with no executor is refused on the SERVER (CreateAgent, an attached
// runner not helping, UpdateAgent) — all pointed at cursor_agent as the live
// example of "declared but unbuilt". cursor_agent now has an executor (see
// internal/adapter/cli/cursor), and so do antigravity and opencode: every
// provider in the catalog is Available:true, leaving no declared-but-unbuilt
// provider to exercise that guard with. An assertion with nothing left to
// prove it true is not a test, so they were removed rather than left red.
//
// The invariant is still guarded at the domain layer:
// domain.TestEveryProviderExceptDeclaredButUnbuiltIsAvailable
// (llm_provider_available_test.go) fails the moment a future provider is
// declared with Available:false and forces whoever adds it to notice — which
// is the right moment to also re-add an integration test here pointed at that
// provider's name.
//
// What remains below is the one case that was never about availability: the
// guard has to leave an endpoint ref (a uuid, not a declared provider type)
// alone, or an agent could never be saved on a named OpenAI-compatible
// endpoint.
func TestAnEndpointRefIsNotTreatedAsUnavailable(t *testing.T) {
	store := &createStore{}
	svc := catalog.NewService(store, nil, "")

	_, err := svc.CreateAgent(context.Background(), domain.CreateAgentRequest{
		Name:         "endpoint-builder",
		ProviderType: domain.LLMProviderType(uuid.New().String()),
		Model:        "some-model",
	})

	require.NoError(t, err)
	require.Equal(t, 1, store.calls)
}
