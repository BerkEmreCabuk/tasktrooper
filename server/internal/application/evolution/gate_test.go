package evolution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeManager struct {
	created  []string
	updated  []string
	deleted  []string
	rcreated []string
	rupdated []string
}

func (f *fakeManager) CreateSkillForAgent(_ context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) (domain.Skill, error) {
	f.created = append(f.created, req.Name)
	return domain.Skill{ID: uuid.New(), AgentID: agentID, Name: req.Name, Content: req.Content, Enabled: true}, nil
}

func (f *fakeManager) UpdateSkillForAgent(_ context.Context, agentID, skillID uuid.UUID, req domain.UpdateSkillRequest) (domain.Skill, error) {
	f.updated = append(f.updated, req.Name)
	return domain.Skill{ID: skillID, AgentID: agentID, Name: req.Name, Content: req.Content, Enabled: true}, nil
}

func (f *fakeManager) DeleteSkillForAgent(_ context.Context, _, skillID uuid.UUID) error {
	f.deleted = append(f.deleted, skillID.String())
	return nil
}

func (f *fakeManager) CreateRuleForAgent(_ context.Context, agentID uuid.UUID, req domain.CreateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	f.rcreated = append(f.rcreated, req.Name)
	return domain.OrchestratorRule{ID: uuid.New(), AgentID: agentID, Name: req.Name, Content: req.Content}, nil
}

func (f *fakeManager) UpdateRuleForAgent(_ context.Context, agentID, ruleID uuid.UUID, req domain.UpdateOrchestratorRuleRequest) (domain.OrchestratorRule, error) {
	f.rupdated = append(f.rupdated, req.Name)
	return domain.OrchestratorRule{ID: ruleID, AgentID: agentID, Name: req.Name, Content: req.Content}, nil
}

func (f *fakeManager) DeleteRuleForAgent(_ context.Context, _, _ uuid.UUID) error { return nil }

func skillMap(names ...string) map[string]domain.Skill {
	out := map[string]domain.Skill{}
	for _, n := range names {
		id := uuid.New()
		out[id.String()] = domain.Skill{ID: id, Name: n, Enabled: true}
	}
	return out
}

func TestSkillBudgetRejectsCreateAtCap(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxSkillChanges: 3, MaxSkillsPerAgent: 2}}
	existing := skillMap("alpha", "beta")

	applied := s.applySkillChanges(context.Background(), domain.Agent{ID: uuid.New(), Name: "dev"},
		[]domain.ReflectionSkillChange{{Action: "create", Name: "gamma", Description: "d", Content: "c"}},
		existing, func(domain.AgentEvolutionEvent) {})

	if len(mgr.created) != 0 {
		t.Fatalf("create should be rejected at budget, got %v", mgr.created)
	}
	if len(applied) != 1 || !strings.Contains(applied[0], "rejected") {
		t.Fatalf("rejection not reported: %v", applied)
	}
}

func TestSkillBudgetFreesAfterDelete(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxSkillChanges: 5, MaxSkillsPerAgent: 2}}
	existing := skillMap("alpha", "beta")
	var doomed string
	for id, sk := range existing {
		if sk.Name == "beta" {
			doomed = id
		}
	}

	s.applySkillChanges(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.ReflectionSkillChange{
			{Action: "delete", SkillID: doomed},
			{Action: "create", Name: "gamma", Description: "d", Content: "c"},
		}, existing, func(domain.AgentEvolutionEvent) {})

	if len(mgr.created) != 1 {
		t.Fatalf("create after delete should fit the budget, got %v", mgr.created)
	}
}

func TestCreateOnExistingNameBecomesUpdate(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxSkillChanges: 3, MaxSkillsPerAgent: 10}}

	s.applySkillChanges(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.ReflectionSkillChange{{Action: "create", Name: "Alpha", Description: "d", Content: "new body"}},
		skillMap("alpha"), func(domain.AgentEvolutionEvent) {})

	if len(mgr.created) != 0 || len(mgr.updated) != 1 {
		t.Fatalf("same-name create should merge into an update: created=%v updated=%v", mgr.created, mgr.updated)
	}
}

func TestRuleBudgetRejectsCreateAtCap(t *testing.T) {
	mgr := &fakeManager{}
	s := &Service{manager: mgr, cfg: domain.EvolutionConfig{MaxRuleChanges: 3, MaxRulesPerAgent: 1}}
	id := uuid.New()
	existing := map[string]domain.OrchestratorRule{id.String(): {ID: id, Name: "only"}}

	applied := s.applyRuleChanges(context.Background(), domain.Agent{ID: uuid.New()},
		[]domain.ReflectionRuleChange{{Action: "create", Name: "second", Content: "c"}},
		existing, func(domain.AgentEvolutionEvent) {})

	if len(mgr.rcreated) != 0 || len(applied) != 1 || !strings.Contains(applied[0], "rejected") {
		t.Fatalf("rule create should be rejected at budget: created=%v applied=%v", mgr.rcreated, applied)
	}
}

func TestParseGateVerdict(t *testing.T) {
	v, err := parseGateVerdict(`{"keep":false,"reason":"golden dropped"}`)
	if err != nil || v.Keep || v.Reason != "golden dropped" {
		t.Fatalf("clean verdict parse failed: %+v %v", v, err)
	}
	v, err = parseGateVerdict("```json\n{\"keep\":true,\"reason\":\"better\"}\n```")
	if err != nil || !v.Keep {
		t.Fatalf("fenced verdict parse failed: %+v %v", v, err)
	}
	if _, err := parseGateVerdict("no json"); err == nil {
		t.Fatal("expected error on garbage verdict")
	}
}

func TestCatalogChangeEventsSkipsMemoriesAndReverts(t *testing.T) {
	events := []domain.AgentEvolutionEvent{
		{ChangeType: domain.EvolutionChangeSkillCreated, TargetKind: domain.EvolutionTargetSkill},
		{ChangeType: domain.EvolutionChangeMemoryCreated, TargetKind: domain.EvolutionTargetMemory},
		{ChangeType: domain.EvolutionChangeRevert, TargetKind: domain.EvolutionTargetRule},
		{ChangeType: domain.EvolutionChangeRuleUpdated, TargetKind: domain.EvolutionTargetRule},
	}
	got := catalogChangeEvents(events)
	if len(got) != 2 {
		t.Fatalf("expected only the skill and rule changes, got %d", len(got))
	}
}

func TestProposesCatalogChange(t *testing.T) {
	memoryOnly := domain.ReflectionOutput{Memories: []domain.ReflectionMemoryChange{{Action: "create"}}}
	if proposesCatalogChange(memoryOnly) {
		t.Error("memory-only output should not arm the gate")
	}
	withSkill := domain.ReflectionOutput{Skills: []domain.ReflectionSkillChange{{Action: "create"}}}
	if !proposesCatalogChange(withSkill) {
		t.Error("skill change should arm the gate")
	}
}

func TestJudgeRoutingPrefersJudgeModel(t *testing.T) {
	s := &Service{cfg: domain.EvolutionConfig{Model: "cheap", JudgeModel: "strict", JudgeProviderType: "openai"}}
	model, provider := s.judgeRouting(domain.Agent{Model: "agent-model", ProviderType: "anthropic"})
	if model != "strict" || provider != domain.LLMProviderType("openai") {
		t.Fatalf("judge routing wrong: %s / %s", model, provider)
	}
}

func TestReflectionPromptCarriesBudgetAndGate(t *testing.T) {
	cfg := domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 5,
		MaxSkillsPerAgent: 25, MaxRulesPerAgent: 15, GoldenGate: true}
	prompt := reflectionSystemPrompt(domain.Agent{Name: "dev", SelfEvolutionEnabled: true}, cfg)
	if !strings.Contains(prompt, "CONSOLIDATION FIRST") {
		t.Error("prompt missing consolidation instruction")
	}
	if !strings.Contains(prompt, "at most 25 skills") {
		t.Error("prompt missing standing budget")
	}
	if !strings.Contains(prompt, "rolls the whole set back") {
		t.Error("prompt missing golden gate warning")
	}
}

// --- the golden gate with no grader ----------------------------------------

// erroringLLM answers every call with one error. Stands in for a judge (or a
// golden suite) running on an agent whose engine cannot serve the call.
type erroringLLM struct {
	err   error
	calls int
}

func (e *erroringLLM) Chat(_ context.Context, _ domain.AgentRequest) (domain.AgentResponse, error) {
	e.calls++
	return domain.AgentResponse{}, e.err
}

func (e *erroringLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	e.calls++
	return domain.AgentResponse{}, e.err
}
func (e *erroringLLM) Models(context.Context) ([]string, error) { return nil, nil }
func (e *erroringLLM) Embed(context.Context, string, string) ([]float32, error) {
	return nil, nil
}

// A LOAD-BEARING step failing rather than appearing to succeed.
//
// The golden gate decides whether an agent keeps the skills and rules it just
// rewrote about itself. Its fallback for an unreachable judge is "keep only if
// the suite did not get worse", which is sound — as long as the suite RAN.
//
// It did not, and the old silent provider fallback is why: the judge call was
// rerouted to the tenant's active default HTTP provider, which for this tenant
// was a dead `gemini-2.0-flash`. So the judge failed, `after >= before` held
// trivially, and the change set was kept with the reason "kept on
// non-regression" — a phrase that reads on the dashboard exactly like evidence,
// attached to a grading that never happened.
//
// With no grader, the answer is REVERT.
func TestGoldenGateRevertsWhenTheJudgeCannotRun(t *testing.T) {
	refusal := domain.ErrHostExecutedProvider(domain.LLMProviderClaudeCode)
	llm := &erroringLLM{err: refusal}
	svc := &Service{llm: llm}

	// Deliberately the shape that used to be "kept on non-regression": the suite
	// ran, and the rate held exactly level.
	before := goldenRun{Rate: 0.8, Evaluated: 5}
	after := goldenRun{Rate: 0.8, Evaluated: 5}

	verdict := svc.judgeGoldenGate(context.Background(), domain.Agent{
		Name: "backend-developer", ProviderType: domain.LLMProviderClaudeCode, Model: "opus",
	}, before, after, []string{"rewrote the retry skill"})

	if verdict.Keep {
		t.Fatal("an ungraded change set was kept: the gate reported a pass it never ran")
	}
	if !strings.Contains(verdict.Reason, "could not run") {
		t.Errorf("the reason must say the judge could not run, got %q", verdict.Reason)
	}
	if !strings.Contains(verdict.Reason, "Claude Code") {
		t.Errorf("the reason must name the engine so the operator can fix it, got %q", verdict.Reason)
	}
}

// A transient judge failure keeps the old, deliberate behaviour: the suite ran,
// it did not regress, and one bad minute from the judge is not a reason to throw
// away a change set the evidence supports.
func TestGoldenGateStillKeepsOnNonRegressionForATransientJudgeFailure(t *testing.T) {
	svc := &Service{llm: &erroringLLM{err: errors.New("connection reset by peer")}}

	verdict := svc.judgeGoldenGate(context.Background(), domain.Agent{Name: "a"},
		goldenRun{Rate: 0.8, Evaluated: 5}, goldenRun{Rate: 0.9, Evaluated: 5}, []string{"c"})

	if !verdict.Keep {
		t.Fatalf("a transient judge failure must not revert an improvement: %q", verdict.Reason)
	}
	if !strings.Contains(verdict.Reason, "non-regression") {
		t.Errorf("reason = %q, want the non-regression fallback", verdict.Reason)
	}
}

// The suite itself being unrunnable is the same verdict, reached earlier and
// without spending a judge call: two rates of zero over zero tasks satisfy
// `after >= before` perfectly, and that is not evidence of anything.
func TestGoldenGateRevertsWhenTheSuiteCouldNotRunAtAll(t *testing.T) {
	llm := &erroringLLM{err: errors.New("should not be reached")}
	svc := &Service{llm: llm}
	refusal := domain.ErrHostExecutedProvider(domain.LLMProviderClaudeCode)

	verdict := svc.judgeGoldenGate(context.Background(), domain.Agent{Name: "a"},
		goldenRun{Unservable: refusal}, goldenRun{Unservable: refusal}, []string{"c"})

	if verdict.Keep {
		t.Fatal("a change set was kept on a suite that never evaluated a single task")
	}
	if llm.calls != 0 {
		t.Errorf("the judge was called %d times for an unmeasured change set; there is nothing to grade", llm.calls)
	}
	if !strings.Contains(verdict.Reason, "could not run") {
		t.Errorf("reason = %q, want it to say the suite could not run", verdict.Reason)
	}
}

// A real regression still reverts before anything else is consulted — the
// deterministic floor the gate has always had must not have moved.
func TestGoldenGateStillRevertsARealRegressionFirst(t *testing.T) {
	llm := &erroringLLM{err: errors.New("should not be reached")}
	svc := &Service{llm: llm}

	verdict := svc.judgeGoldenGate(context.Background(), domain.Agent{Name: "a"},
		goldenRun{Rate: 0.9, Evaluated: 5}, goldenRun{Rate: 0.5, Evaluated: 5}, []string{"c"})

	if verdict.Keep {
		t.Fatal("a dropped pass rate must always revert")
	}
	if llm.calls != 0 {
		t.Errorf("the judge was consulted on a regression (%d calls); the floor is deterministic", llm.calls)
	}
}
