package llmprovider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// stubEmbeddingHost stands in for the Mac.
type stubEmbeddingHost struct {
	status domain.EmbeddingHostStatus
	err    error
	calls  int
}

func (s *stubEmbeddingHost) Probe(context.Context) (domain.EmbeddingHostStatus, error) {
	s.calls++
	return s.status, s.err
}

func (s *stubEmbeddingHost) Available() bool { return true }

// The bug this whole file exists for: `local_runner` is not a catalog provider
// and not an llm_endpoints uuid, so the model listing fell through to the
// endpoint branch and asked Postgres to parse it as one. The user was shown
// `invalid input syntax for type uuid: "local_runner" (SQLSTATE 22P02)` under
// a model picker.
func TestListEmbeddingModelsForLocalRunnerReturnsThePinnedModel(t *testing.T) {
	svc := llmprovider.NewService(newControlPlaneStore(), nil, nil, 0, nil)
	svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")

	models, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderLocalRunner)
	require.NoError(t, err)
	require.Equal(t, []string{domain.PinnedLocalEmbeddingModel}, models,
		"the Mac serves exactly one pinned model, so that is the whole honest catalog")
}

// The listing must not depend on the Mac being reachable. An empty picker next
// to a warning is the state the user was already in; the model is known from
// the pin whether or not LM Studio is running, and readiness is a separate
// question (EmbeddingStatus).
func TestListEmbeddingModelsForLocalRunnerNeverAsksTheMac(t *testing.T) {
	svc := llmprovider.NewService(newControlPlaneStore(), nil, nil, 0, nil)
	svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")
	host := &stubEmbeddingHost{err: errors.New("tunnel down")}
	svc.SetEmbeddingHost(host)

	models, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderLocalRunner)
	require.NoError(t, err)
	require.Equal(t, []string{domain.PinnedLocalEmbeddingModel}, models)
	require.Zero(t, host.calls)
}

// Nothing on this path may hand a non-uuid string to a uuid column, and
// nothing it says may name storage.
func TestListEmbeddingModelsNeverSurfacesAStorageError(t *testing.T) {
	svc, _ := newEmbeddingModelsService(t, `{"data":[]}`)

	for _, ref := range []string{"local_runner", "not-an-endpoint", "'; drop table --"} {
		_, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderType(ref))
		require.Error(t, err)
		lower := strings.ToLower(err.Error())
		for _, leak := range []string{"sqlstate", "uuid", "llm_endpoint", "syntax"} {
			require.NotContains(t, lower, leak, "ref %q leaked storage wording: %s", ref, err)
		}
	}
}

// A deployment with no control plane has no Mac to embed on, and says so in a
// sentence about embeddings.
func TestListEmbeddingModelsForLocalRunnerWithoutAControlPlane(t *testing.T) {
	svc := llmprovider.NewService(newControlPlaneStore(), nil, nil, 0, nil)

	_, err := svc.ListEmbeddingModels(context.Background(), domain.LLMProviderLocalRunner)
	require.Error(t, err)
	require.Contains(t, err.Error(), "control plane")
}

// List() resolves a blank stored value to local_runner for display, so the
// settings page shows it as the current selection — and saving that selection
// used to be refused with "invalid provider ref". The one choice a user could
// not make was the one already in force.
func TestSetEmbeddingAcceptsLocalRunner(t *testing.T) {
	store := newControlPlaneStore()
	svc := llmprovider.NewService(store, nil, nil, 0, nil)
	svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")

	out, err := svc.SetEmbedding(context.Background(), domain.LLMProviderLocalRunner, domain.PinnedLocalEmbeddingModel)
	require.NoError(t, err)
	require.Equal(t, domain.LLMProviderLocalRunner, out.EmbeddingProvider)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, out.EmbeddingModel)
	require.True(t, out.EmbeddingOnMemberMac)
}

// The model is a pin on BOTH sides. Storing another one would be accepted here
// and refused by the Mac on every embedding call afterwards, with the settings
// page still showing the choice as saved.
func TestSetEmbeddingRefusesAnotherModelOnTheMac(t *testing.T) {
	svc := llmprovider.NewService(newControlPlaneStore(), nil, nil, 0, nil)
	svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")

	_, err := svc.SetEmbedding(context.Background(), domain.LLMProviderLocalRunner, "bge-m3")
	require.Error(t, err)
	require.Contains(t, err.Error(), domain.PinnedLocalEmbeddingModel)
}

// The page's old conclusion — "no connected provider can produce embeddings" —
// was computed from a catalog local_runner is deliberately absent from. This
// flag is what replaces that derivation.
func TestListReportsEmbeddingOnMemberMac(t *testing.T) {
	svc := llmprovider.NewService(newControlPlaneStore(), nil, nil, 0, nil)

	out, err := svc.List(context.Background())
	require.NoError(t, err)
	require.False(t, out.EmbeddingOnMemberMac, "no control plane means no Mac in the embedding path")

	svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")
	out, err = svc.List(context.Background())
	require.NoError(t, err)
	require.True(t, out.EmbeddingOnMemberMac)
	require.Equal(t, domain.LLMProviderLocalRunner, out.EmbeddingProvider)
}

func TestEmbeddingStatusReportsTheHostState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state domain.EmbeddingHostState
	}{
		{"ready", domain.EmbeddingHostReady},
		{"no mac", domain.EmbeddingHostNoMac},
		{"lm studio down", domain.EmbeddingHostLMStudioDown},
		{"model missing", domain.EmbeddingHostModelMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := llmprovider.NewService(newControlPlaneStore(), nil, nil, 0, nil)
			svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")
			svc.SetEmbeddingHost(&stubEmbeddingHost{status: domain.EmbeddingHostStatus{State: tc.state, Detail: "d"}})

			out, err := svc.EmbeddingStatus(context.Background())
			require.NoError(t, err)
			require.True(t, out.OnMemberMac)
			require.Equal(t, domain.LLMProviderLocalRunner, out.Provider)
			require.Equal(t, domain.PinnedLocalEmbeddingModel, out.Model)
			require.Equal(t, domain.PinnedLocalEmbeddingDimensions, out.Dimensions)
			require.Equal(t, tc.state, out.Host.State)
		})
	}
}

// A Mac that cannot be asked is a third thing next to ready and broken. The
// provider and model are still true, and the probe's own wording — which is
// about this process, not about the laptop — must not reach the user.
func TestEmbeddingStatusReportsUnknownWhenTheMacCannotBeAsked(t *testing.T) {
	svc := llmprovider.NewService(newControlPlaneStore(), nil, nil, 0, nil)
	svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")
	svc.SetEmbeddingHost(&stubEmbeddingHost{err: errors.New("runner: no member to address")})

	out, err := svc.EmbeddingStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, domain.EmbeddingHostUnknown, out.Host.State)
	require.Empty(t, out.Host.Detail)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, out.Model)
}

// An HTTP embedding provider has no laptop in its path, so the tunnel is never
// dialled — a settings page load must not spend seconds learning nothing.
func TestEmbeddingStatusDoesNotAskTheMacForAnHTTPProvider(t *testing.T) {
	store := newControlPlaneStore()
	store.configs[domain.LLMProviderOpenAI] = domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderOpenAI, BaseURL: "https://api.openai.com/v1", Configured: true,
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)
	svc.SetControlPlaneEndpoint("https://tasktrooper.ai", "signing-key")
	host := &stubEmbeddingHost{status: domain.EmbeddingHostStatus{State: domain.EmbeddingHostReady}}
	svc.SetEmbeddingHost(host)

	_, err := svc.SetEmbedding(context.Background(), domain.LLMProviderOpenAI, "text-embedding-3-small")
	require.NoError(t, err)

	out, err := svc.EmbeddingStatus(context.Background())
	require.NoError(t, err)
	require.False(t, out.OnMemberMac)
	require.Equal(t, domain.EmbeddingHostUnknown, out.Host.State)
	require.Zero(t, host.calls)
}

// The two endpoint CRUD paths were siblings of the same bug: an unvalidated
// :id straight into a uuid column, with the store's error printed verbatim.
func TestEndpointWritesNeverSurfaceAStorageError(t *testing.T) {
	svc, _ := newEmbeddingModelsService(t, `{"data":[]}`)

	_, updateErr := svc.UpdateEndpoint(context.Background(), "local_runner", domain.SaveLLMEndpointRequest{})
	_, deleteErr := svc.DeleteEndpoint(context.Background(), "local_runner")
	for _, err := range []error{updateErr, deleteErr} {
		require.Error(t, err)
		lower := strings.ToLower(err.Error())
		for _, leak := range []string{"sqlstate", "uuid", "llm_endpoint"} {
			require.NotContains(t, lower, leak)
		}
	}
}
