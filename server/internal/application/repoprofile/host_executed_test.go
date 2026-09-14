package repoprofile

import (
	"context"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// refusingLoop is an HTTP loop that fails the test if it is ever asked. The
// point of these tests is that a host-executed architect never reaches it.
type refusingLoop struct{ t *testing.T }

func (l refusingLoop) Run(context.Context, []domain.Message, string, domain.LLMProviderType, domain.ToolPolicy, ...agent.RunOption) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code architect must never reach the HTTP agent loop")
	return domain.AgentResponse{}, nil
}

func (l refusingLoop) RunTask(context.Context, []domain.Message, string, domain.LLMProviderType, domain.ToolPolicy, ...agent.RunOption) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code architect must never reach the HTTP agent loop")
	return domain.AgentResponse{}, nil
}

func (l refusingLoop) RunStream(context.Context, []domain.Message, string, domain.LLMProviderType, domain.ToolPolicy, func(string), ...agent.RunOption) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code architect must never reach the HTTP agent loop")
	return domain.AgentResponse{}, nil
}

// cliStub is the CLI executor. The section it writes is the refresh's success
// signal, so writing it from here proves the run reached an engine at all.
type cliStub struct {
	mu     sync.Mutex
	calls  int
	last   domain.TaskExecution
	onCall func(ctx context.Context)
}

func (c *cliStub) Supports(p domain.LLMProviderType) bool { return p == domain.LLMProviderClaudeCode }

func (c *cliStub) Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	c.mu.Lock()
	c.calls++
	c.last = req
	onCall := c.onCall
	c.mu.Unlock()
	if onCall != nil {
		onCall(ctx)
	}
	return domain.AgentResponse{Message: domain.Message{Content: "profile written"}}, nil
}

func (c *cliStub) snapshot() (int, domain.TaskExecution) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.last
}

// A profile refresh whose system-architect runs on claude_code used to be
// impossible: the only engine it could ask was the HTTP loop, which refuses
// that provider, so every refresh logged two "pass errored" warnings and ended
// with the derived half of the profile and no judgment half at all.
func TestRefreshRunsOnTheHostExecutor(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	var svc *Service
	ex := &cliStub{onCall: func(ctx context.Context) {
		if _, err := writeSection(ctx, svc, id, "conventions from the CLI"); err != nil {
			t.Errorf("profile write-through from the CLI session failed: %v", err)
		}
	}}
	router := agent.NewRouter(refusingLoop{t: t})
	router.SetTaskExecutor(ex)

	svc = NewService(store, profiles, router)
	svc.SetAgentLister(func(context.Context) ([]domain.Agent, error) {
		return []domain.Agent{{
			Name: architectAgentName, ProviderType: domain.LLMProviderClaudeCode, Model: "opus",
		}}, nil
	})

	if err := svc.Refresh(context.Background(), id, "manual"); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	calls, req := ex.snapshot()
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1 (the section landed on the first pass)", calls)
	}
	// The refresh runs read-only against the shared working copy, which is what
	// the run context carries and therefore where the CLI session is started.
	if req.WorkDir != store.repo.RootPath {
		t.Fatalf("cli work dir = %q, want the repository root %q", req.WorkDir, store.repo.RootPath)
	}
	if req.Provider != domain.LLMProviderClaudeCode || req.Model != "opus" {
		t.Fatalf("the architect's own engine did not travel: provider=%q model=%q", req.Provider, req.Model)
	}
	if req.TaskKey == "" {
		t.Fatal("the CLI session must be labelled so it is identifiable in the logs")
	}
}

// With no runner on this host the refresh still fails the way it always did —
// with a warning per pass and an error naming the missing section — rather than
// by sending an architect's exploration run to somebody else's endpoint.
func TestRefreshFailsHonestlyWithNoRunner(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	router := agent.NewRouter(refusingLoop{t: t})

	svc := NewService(store, profiles, router)
	svc.SetAgentLister(func(context.Context) ([]domain.Agent, error) {
		return []domain.Agent{{
			Name: architectAgentName, ProviderType: domain.LLMProviderClaudeCode, Model: "opus",
		}}, nil
	})

	if err := svc.Refresh(context.Background(), id, "manual"); err == nil {
		t.Fatal("a refresh with no engine to run on must not report success")
	}
}
