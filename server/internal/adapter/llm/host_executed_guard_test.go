package llm

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// spyClient stands in for the client a redirected call lands on, and records
// the request it was given — which is where the blanked model is asserted.
type spyClient struct {
	called bool
	last   domain.AgentRequest
}

var _ port.LLMClient = (*spyClient)(nil)

func (s *spyClient) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	s.called, s.last = true, req
	return domain.AgentResponse{}, nil
}

func (s *spyClient) ChatStream(_ context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	s.called, s.last = true, req
	return domain.AgentResponse{}, nil
}

func (s *spyClient) Models(context.Context) ([]string, error) { return nil, nil }

func (s *spyClient) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

// captureLogs points the global logger at a buffer for the duration of a test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = previous })
	return &buf
}

// An AGENTIC request — one carrying tools — is still refused outright. This is
// the hazard the guard was added for: such a request is a RUN that should have
// been dispatched to the CLI by agent.Router, and sending it on would execute
// the user's task on an engine they did not choose, under another vendor's key.
func TestMultiClientRefusesAgenticHostExecutedRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*MultiProviderClient, domain.AgentRequest) error
	}{
		{"chat", func(m *MultiProviderClient, req domain.AgentRequest) error {
			_, err := m.Chat(context.Background(), req)
			return err
		}},
		{"chat stream", func(m *MultiProviderClient, req domain.AgentRequest) error {
			_, err := m.ChatStream(context.Background(), req, func(string) {})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fallback := &spyClient{}
			multi := NewMultiProviderClient(fallback, StaticResolver(staticSet(domain.LLMProviderOpenAI, nil)))

			err := tc.call(multi, domain.AgentRequest{
				ProviderType: domain.LLMProviderClaudeCode,
				Model:        "opus",
				Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
				Tools: []domain.ToolDefinition{
					{Type: "function", Function: domain.FunctionDefinition{Name: "read_file"}},
				},
			})

			if err == nil {
				t.Fatal("expected a refusal for an agentic run on a provider that is a local process")
			}
			if fallback.called {
				t.Fatal("the run reached another provider's endpoint on another provider's key")
			}
			if !strings.Contains(err.Error(), "local runner host") {
				t.Fatalf("expected the sentence that names where this agent can run, got %q", err)
			}
		})
	}
}

// A UTILITY call — no tools, and often a JSON schema the CLI could never
// produce — is REFUSED, and the refusal reaches nobody else.
//
// This test was the inverse of itself until the fallback was removed. It used to
// assert that such a call was rerouted to the active default HTTP
// provider with the model blanked, on the reasoning that the CLI could not serve
// it anyway so a refusal only deleted the feature.
//
// The reasoning was sound and the conclusion was wrong. The default provider is
// one the operator did not choose FOR THIS AGENT, and its health has nothing to
// do with the health of anything they did choose: for this test it was a dead
// `gemini-2.0-flash`, and an unpaid Mistral before that. So every reroute
// converted "this agent cannot serve this step" — true,
// specific, fixable — into a 404 from a provider nobody was thinking about. The
// fallback saved no call and made every failure harder to read.
//
// What is asserted now is the whole contract: it fails, the message names the
// step, the engine, the schema reason and the fix, and NO other provider is
// touched.
func TestMultiClientRefusesUtilityCallsOnHostExecutedProviders(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*MultiProviderClient, domain.AgentRequest) error
	}{
		{"chat", func(m *MultiProviderClient, req domain.AgentRequest) error {
			_, err := m.Chat(context.Background(), req)
			return err
		}},
		{"chat stream", func(m *MultiProviderClient, req domain.AgentRequest) error {
			_, err := m.ChatStream(context.Background(), req, func(string) {})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			def := &spyClient{}
			multi := NewMultiProviderClient(nil, StaticResolver(staticSet(domain.LLMProviderOpenAI,
				map[domain.LLMProviderType]port.LLMClient{domain.LLMProviderOpenAI: def})))

			err := tc.call(multi, domain.AgentRequest{
				ProviderType:   domain.LLMProviderClaudeCode,
				Model:          "sonnet[1m]",
				Messages:       []domain.Message{{Role: domain.RoleUser, Content: "grade this"}},
				ResponseFormat: domain.JSONSchemaResponseFormat("golden_gate_verdict", map[string]any{"type": "object"}),
			})
			if err == nil {
				t.Fatal("expected a refusal: a toolless call on a host-executed provider has nowhere legitimate to go")
			}
			// The part that matters most. A configured default provider is
			// present and healthy here precisely so the test can prove the call
			// does NOT drift onto it.
			if def.called {
				t.Fatal("the utility call reached the default provider; the silent fallback is back")
			}
			if !errors.Is(err, domain.ErrHostExecutedUnservable) {
				t.Fatalf("the refusal must be recognisable to callers (llmretry stops retrying on it), got %#v", err)
			}

			// Every clause the operator needs: which step, which engine, the
			// concrete reason, and the two things they can do about it.
			for _, want := range []string{
				"golden_gate_verdict",
				"Claude Code",
				"JSON-schema response format",
				"Configure an API-backed HTTP provider",
				"turn this step off",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the refusal does not say %q — the operator cannot act on it.\ngot: %v", want, err)
				}
			}

			line := logs.String()
			for _, want := range []string{"claude_code", "sonnet[1m]", "golden_gate_verdict", "host_executed_guard_test.go"} {
				if !strings.Contains(line, want) {
					t.Fatalf("the refusal log does not mention %q; a step that silently never runs is the bug.\n%s", want, line)
				}
			}
			if !strings.Contains(line, `"level":"warn"`) {
				t.Fatalf("the refusal must be logged at warn, got: %s", line)
			}
		})
	}
}

// A plain-text utility call is refused too, and its message names the call SITE
// rather than a schema — there is no schema to name, and "the model call at
// <file:line>" is at least unambiguous.
//
// The schema clause must be ABSENT here: claiming a JSON-schema reason for a
// call that asked for prose would send the operator looking for a constraint
// that was never requested.
func TestMultiClientRefusalNamesTheCallSiteWhenThereIsNoSchema(t *testing.T) {
	def := &spyClient{}
	multi := NewMultiProviderClient(nil, StaticResolver(staticSet(domain.LLMProviderOpenAI,
		map[domain.LLMProviderType]port.LLMClient{domain.LLMProviderOpenAI: def})))

	_, err := multi.Chat(context.Background(), domain.AgentRequest{
		ProviderType: domain.LLMProviderClaudeCode,
		Model:        "opus",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "summarize this"}},
	})
	if err == nil {
		t.Fatal("expected a refusal for a toolless plain-text call on a host-executed provider")
	}
	if def.called {
		t.Fatal("the call reached the default provider")
	}
	if !strings.Contains(err.Error(), "host_executed_guard_test.go") {
		t.Fatalf("a schema-less refusal must name the call site, got %q", err)
	}
	if strings.Contains(err.Error(), "JSON-schema") {
		t.Fatalf("a plain-text call must not be blamed on a schema it never asked for: %q", err)
	}
}

// The refusal does not depend on there being a default provider, or on what it
// is. Removing the fallback removed the whole question: nothing is consulted
// about where to send the call, because the call is not being sent.
func TestMultiClientRefusesRegardlessOfTheConfiguredDefault(t *testing.T) {
	for _, tc := range []struct {
		name    string
		build   func() (*MultiProviderClient, *spyClient)
		wantErr string
	}{
		{
			name: "no default configured at all",
			build: func() (*MultiProviderClient, *spyClient) {
				return NewMultiProviderClient(nil, StaticResolver(staticSet(domain.LLMProviderOpenAI, nil))), nil
			},
		},
		{
			name: "the default is itself host-executed",
			build: func() (*MultiProviderClient, *spyClient) {
				spy := &spyClient{}
				return NewMultiProviderClient(spy, StaticResolver(staticSet(domain.LLMProviderClaudeCode, nil))), spy
			},
		},
		{
			name: "a perfectly healthy HTTP default is available",
			build: func() (*MultiProviderClient, *spyClient) {
				spy := &spyClient{}
				return NewMultiProviderClient(spy, StaticResolver(staticSet(domain.LLMProviderOpenAI, nil))), spy
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			multi, spy := tc.build()

			_, err := multi.Chat(context.Background(), domain.AgentRequest{
				ProviderType: domain.LLMProviderClaudeCode,
				Model:        "opus",
				Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
			})
			if err == nil {
				t.Fatal("expected a refusal whatever the default is")
			}
			if spy != nil && spy.called {
				t.Fatal("the call reached a client; the refusal must happen before any client is resolved")
			}
			if !errors.Is(err, domain.ErrHostExecutedUnservable) {
				t.Fatalf("expected the recognisable refusal, got %v", err)
			}
		})
	}
}

// An endpoint-backed provider with no configured client still resolves to the
// fallback client — the refusal must key on the provider being host-executed,
// not on the client map being empty. This is the guard against the refusal
// widening into "anything unconfigured is refused", which would break every
// install that runs one client for several providers.
func TestMultiClientStillFallsBackForEndpointProviders(t *testing.T) {
	fallback := &spyClient{}
	multi := NewMultiProviderClient(fallback, StaticResolver(staticSet(domain.LLMProviderOpenAI, nil)))

	if _, err := multi.Chat(context.Background(), domain.AgentRequest{
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "claude-sonnet-4-6",
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallback.called {
		t.Fatal("an endpoint-backed provider still resolves to the fallback")
	}
	// And its model is untouched: only a host-executed redirect blanks one.
	if fallback.last.Model != "claude-sonnet-4-6" {
		t.Fatalf("an ordinary request's model was rewritten: %q", fallback.last.Model)
	}
}
