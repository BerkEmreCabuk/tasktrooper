package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// embedSpy is a minimal port.LLMClient whose Embed is scripted per test — the
// only method these tests exercise.
type embedSpy struct {
	vec    []float32
	err    error
	called bool
}

func (e *embedSpy) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (e *embedSpy) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (e *embedSpy) Models(context.Context) ([]string, error) { return nil, nil }
func (e *embedSpy) Embed(context.Context, string, string) ([]float32, error) {
	e.called = true
	return e.vec, e.err
}

// TestAutoEmbeddingPrefersTheLocalRunnerOverTheChatDefault is point 4 of the
// embeddings rework: a tenant that never chose an embedding provider must end
// up on its own Mac, not on whatever HTTP provider happens to be the chat
// default (which may not even produce embeddings, e.g. Anthropic).
func TestAutoEmbeddingPrefersTheLocalRunnerOverTheChatDefault(t *testing.T) {
	chatDefault := &embedSpy{vec: []float32{9, 9, 9}}
	local := &embedSpy{vec: []float32{1, 2, 3}}

	m := NewMultiProviderClient(nil, StaticResolver(staticSet(domain.LLMProviderAnthropic,
		map[domain.LLMProviderType]port.LLMClient{
			domain.LLMProviderAnthropic:   chatDefault,
			domain.LLMProviderLocalRunner: local,
		})))
	// EmbeddingProvider left unset: "auto".

	vec, err := m.Embed(context.Background(), "text", "")
	require.NoError(t, err)
	require.Equal(t, []float32{1, 2, 3}, vec)
	require.True(t, local.called, "auto must resolve to the tenant's own Mac")
	require.False(t, chatDefault.called, "the chat default must not be asked when the Mac is available")
}

// TestAutoEmbeddingDoesNotFallBackWhenTheLocalRunnerFails is the corruption
// guard: falling through to a different provider on failure would silently
// write a vector from a different model — a different dimension count — into
// the same index as vectors the Mac already wrote. Failing loudly once is the
// point; see domain.EmbeddingProvenanceStale for the other half of this guard.
func TestAutoEmbeddingDoesNotFallBackWhenTheLocalRunnerFails(t *testing.T) {
	chatDefault := &embedSpy{vec: []float32{9, 9, 9}}
	local := &embedSpy{err: domain.ErrEmbeddingRunnerNotAttached()}

	m := NewMultiProviderClient(nil, StaticResolver(staticSet(domain.LLMProviderAnthropic,
		map[domain.LLMProviderType]port.LLMClient{
			domain.LLMProviderAnthropic:   chatDefault,
			domain.LLMProviderLocalRunner: local,
		})))

	_, err := m.Embed(context.Background(), "text", "")
	require.Error(t, err)
	require.True(t, errors.Is(err, domain.ErrRunnerNotAttached))
	require.False(t, chatDefault.called, "a Mac failure must not silently fall through to a different model's provider")
}

// An explicit pin still wins over the Mac — a tenant's deliberate choice is
// never overridden by the automatic default.
func TestExplicitEmbeddingPinStillWinsOverTheLocalRunner(t *testing.T) {
	chosen := &embedSpy{vec: []float32{4, 5, 6}}
	local := &embedSpy{vec: []float32{1, 2, 3}}

	set := staticSet(domain.LLMProviderAnthropic, map[domain.LLMProviderType]port.LLMClient{
		domain.LLMProviderOpenAI:      chosen,
		domain.LLMProviderLocalRunner: local,
	})
	set.EmbeddingProvider = domain.LLMProviderOpenAI
	m := NewMultiProviderClient(nil, StaticResolver(set))

	vec, err := m.Embed(context.Background(), "text", "")
	require.NoError(t, err)
	require.Equal(t, []float32{4, 5, 6}, vec)
	require.False(t, local.called)
}

// Without a local runner client registered at all (self-hosted/desktop, or
// cloud mode never wired to a control plane), auto keeps its old behaviour:
// the chat default serves embeddings, unchanged.
func TestAutoEmbeddingFallsBackToChatDefaultWhenNoLocalRunnerIsRegistered(t *testing.T) {
	chatDefault := &embedSpy{vec: []float32{7, 8, 9}}

	m := NewMultiProviderClient(nil, StaticResolver(staticSet(domain.LLMProviderAnthropic,
		map[domain.LLMProviderType]port.LLMClient{domain.LLMProviderAnthropic: chatDefault})))

	vec, err := m.Embed(context.Background(), "text", "")
	require.NoError(t, err)
	require.Equal(t, []float32{7, 8, 9}, vec)
	require.True(t, chatDefault.called)
}

// staticSet builds a one-tenant ProviderSet, which is what every test in this
// package wants: these exercise routing and pacing, not tenancy. The
// cross-tenant behaviour has its own test — see tenant_isolation_test.go.
func staticSet(def domain.LLMProviderType, clients map[domain.LLMProviderType]port.LLMClient) ProviderSet {
	return ProviderSet{Clients: clients, Default: def}
}
