package evolution

import (
	"context"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// refusingLoop is the HTTP loop, which must never be reached for an agent whose
// engine is a local process.
type refusingLoop struct{ t *testing.T }

func (l refusingLoop) Run(context.Context, []domain.Message, string, domain.LLMProviderType, domain.ToolPolicy, ...agent.RunOption) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code agent's reflection must never reach the HTTP agent loop")
	return domain.AgentResponse{}, nil
}

func (l refusingLoop) RunTask(context.Context, []domain.Message, string, domain.LLMProviderType, domain.ToolPolicy, ...agent.RunOption) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code agent's reflection must never reach the HTTP agent loop")
	return domain.AgentResponse{}, nil
}

func (l refusingLoop) RunStream(context.Context, []domain.Message, string, domain.LLMProviderType, domain.ToolPolicy, func(string), ...agent.RunOption) (domain.AgentResponse, error) {
	l.t.Fatal("a claude_code agent's reflection must never reach the HTTP agent loop")
	return domain.AgentResponse{}, nil
}

type reflectCLI struct {
	mu     sync.Mutex
	calls  int
	last   domain.TaskExecution
	answer string
}

func (c *reflectCLI) Supports(p domain.LLMProviderType) bool {
	return p == domain.LLMProviderClaudeCode
}

func (c *reflectCLI) Execute(_ context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.last = req
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: c.answer}}, nil
}

func (c *reflectCLI) snapshot() (int, domain.TaskExecution) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.last
}

// With web research on, reflection is an agentic run — so it goes to the CLI for
// an agent whose engine is one.
//
// This is the one agentic path with no repository at all: it reads the agent's
// own history and the web. The router gives it a fresh empty scratch directory
// rather than refusing it or letting the session loose in the server's own tree.
func TestReflectionWebResearchRunsOnTheHostExecutor(t *testing.T) {
	cli := &reflectCLI{answer: `{"self_assessment":"ok","skills":[],"rules":[],"memories":[],"reverts":[]}`}
	router := agent.NewRouter(refusingLoop{t: t})
	router.SetTaskExecutor(cli)

	llm := &recordingLLM{answer: `{"self_assessment":"unused","skills":[],"rules":[],"memories":[],"reverts":[]}`}
	svc := &Service{
		llm:       llm,
		agentLoop: router,
		cfg: domain.EvolutionConfig{
			AllowWebResearch: true,
			MaxSkillChanges:  3, MaxRuleChanges: 3, MaxMemoryChanges: 5,
		},
	}

	out, _, err := svc.runLLM(context.Background(), domain.Agent{
		Name: "backend-developer", ProviderType: domain.LLMProviderClaudeCode, Model: "opus",
	}, "evidence text")
	if err != nil {
		t.Fatalf("runLLM: %v", err)
	}
	if out.SelfAssessment != "ok" {
		t.Fatalf("the CLI's answer did not reach the caller: %q", out.SelfAssessment)
	}

	calls, req := cli.snapshot()
	if calls != 1 {
		t.Fatalf("executor calls = %d, want 1", calls)
	}
	if req.Provider != domain.LLMProviderClaudeCode || req.Model != "opus" {
		t.Fatalf("the agent's own engine did not travel: provider=%q model=%q", req.Provider, req.Model)
	}
	if req.WorkDir == "" {
		t.Fatal("a CLI session still needs somewhere to run, even with no repository")
	}
	if len(llm.requests) != 0 {
		t.Fatalf("the web-research branch must not also call the HTTP client (%d calls)", len(llm.requests))
	}
}

// Without web research, reflection is a JSON-schema call the CLI cannot produce
// at all — so it stays on the HTTP client. The loop is not involved either way.
//
// What the client then DOES with a claude_code request is its own decision, made
// in one place, and it changed: it used to reroute the call to the tenant's
// active default provider with the model blanked, and now it refuses it with a
// message naming the step, the engine and the fix. This test deliberately does
// not encode either behaviour — it asserts only that the request leaves here
// intact, with the agent's own provider and its schema attached, so the client
// has everything it needs to make that call. See
// llm.TestMultiClientRefusesUtilityCallsOnHostExecutedProviders for the refusal
// itself, and evolution.TestGoldenGateRevertsWhenTheJudgeCannotRun for what this
// package does with it.
func TestReflectionWithoutWebResearchStaysOnTheLLMClient(t *testing.T) {
	router := agent.NewRouter(refusingLoop{t: t})
	router.SetTaskExecutor(&reflectCLI{})

	llm := &recordingLLM{answer: `{"self_assessment":"ok","skills":[],"rules":[],"memories":[],"reverts":[]}`}
	svc := &Service{
		llm:       llm,
		agentLoop: router,
		cfg:       domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 5},
	}

	if _, _, err := svc.runLLM(context.Background(), domain.Agent{
		Name: "backend-developer", ProviderType: domain.LLMProviderClaudeCode, Model: "opus",
	}, "evidence"); err != nil {
		t.Fatalf("runLLM: %v", err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("llm calls = %d, want 1", len(llm.requests))
	}
	// The provider still travels; blanking the model and picking the default
	// provider is the client's decision, made once, in one place.
	if llm.requests[0].ProviderType != domain.LLMProviderClaudeCode {
		t.Fatalf("provider = %q, want the agent's own; the redirect belongs to the llm client",
			llm.requests[0].ProviderType)
	}
	if llm.requests[0].ResponseFormat == nil {
		t.Fatal("the reflection schema must still be attached")
	}
}

// promotionModel must not pick up whichever agent happened to sort first: the
// classification is the evolution engine's own work, and the zero values mean
// "the tenant's default provider, on that provider's own model".
func TestPromotionModelUsesTheTenantDefault(t *testing.T) {
	s := &Service{}
	model, provider := s.promotionModel()
	if model != "" || provider != "" {
		t.Fatalf("promotion routing = %q/%q, want the tenant default (empty/empty)", model, provider)
	}

	s = &Service{cfg: domain.EvolutionConfig{JudgeModel: "strict", JudgeProviderType: "openai"}}
	model, provider = s.promotionModel()
	if model != "strict" || provider != domain.LLMProviderType("openai") {
		t.Fatalf("the judge override must still win: %q/%q", model, provider)
	}
}
