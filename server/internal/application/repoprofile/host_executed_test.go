package repoprofile

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeArchitectRoles struct{ agentID uuid.UUID }

func (f fakeArchitectRoles) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	return nil, nil
}

func (f fakeArchitectRoles) AgentForPurpose(_ context.Context, purpose domain.RolePurposeKey, _ string) (*uuid.UUID, error) {
	if purpose != domain.PurposeRepoProfiler {
		return nil, nil
	}
	id := f.agentID
	return &id, nil
}

func (f fakeArchitectRoles) AgentArea(context.Context, uuid.UUID) string { return "" }

func (f fakeArchitectRoles) AssigneeForNewTask(_ context.Context, _ domain.TaskType, _ string, requested *uuid.UUID) (*uuid.UUID, error) {
	return requested, nil
}

type fakeArchitectGetter struct{ agent domain.Agent }

func (f fakeArchitectGetter) GetAgent(context.Context, uuid.UUID) (domain.Agent, error) {
	return f.agent, nil
}

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

	architectID := uuid.New()
	svc = NewService(store, profiles, router)
	svc.SetRoleResolver(fakeArchitectRoles{agentID: architectID})
	svc.SetAgentGetter(fakeArchitectGetter{agent: domain.Agent{
		ID: architectID, Name: "system-architect", ProviderType: domain.LLMProviderClaudeCode, Model: "opus",
	}})

	if err := svc.Refresh(context.Background(), id, "manual"); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	calls, req := ex.snapshot()
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1 (the section landed on the first pass)", calls)
	}

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

func TestRefreshFailsHonestlyWithNoRunner(t *testing.T) {
	store, profiles, id := newFixture(t, "", nil)
	router := agent.NewRouter(refusingLoop{t: t})

	architectID := uuid.New()
	svc := NewService(store, profiles, router)
	svc.SetRoleResolver(fakeArchitectRoles{agentID: architectID})
	svc.SetAgentGetter(fakeArchitectGetter{agent: domain.Agent{
		ID: architectID, Name: "system-architect", ProviderType: domain.LLMProviderClaudeCode, Model: "opus",
	}})

	if err := svc.Refresh(context.Background(), id, "manual"); err == nil {
		t.Fatal("a refresh with no engine to run on must not report success")
	}
}
