package catalog

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memCatalogStore struct {
	mu     sync.Mutex
	agents []domain.Agent
	skills []domain.Skill
	rules  []domain.OrchestratorRule
	stacks []domain.TechStack
}

func newMemCatalogStore() *memCatalogStore {
	return &memCatalogStore{}
}

func (m *memCatalogStore) CreateSkill(ctx context.Context, skill domain.Skill) (domain.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if skill.ID == uuid.Nil {
		skill.ID = uuid.New()
	}
	m.skills = append(m.skills, skill)
	return skill, nil
}

func (m *memCatalogStore) GetSkill(ctx context.Context, id uuid.UUID) (domain.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sk := range m.skills {
		if sk.ID == id {
			return sk, nil
		}
	}
	return domain.Skill{}, assert.AnError
}

func (m *memCatalogStore) ListSkills(ctx context.Context) ([]domain.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Skill(nil), m.skills...), nil
}

func (m *memCatalogStore) ListSkillsByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Skill
	for _, sk := range m.skills {
		if sk.AgentID == agentID {
			out = append(out, sk)
		}
	}
	return out, nil
}

func (m *memCatalogStore) GetSkillByAgentAndName(ctx context.Context, agentID uuid.UUID, name string) (domain.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sk := range m.skills {
		if sk.AgentID == agentID && sk.Name == name {
			return sk, nil
		}
	}
	return domain.Skill{}, assert.AnError
}

// Persisted like UpdateRule below. A no-op here made every seed reconcile look
// like it had landed while the store still held the old row, so a test could
// not tell "the reconcile wrote it" from "the reconcile skipped it".
func (m *memCatalogStore) UpdateSkill(ctx context.Context, skill domain.Skill) (domain.Skill, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, sk := range m.skills {
		if sk.ID == skill.ID {
			m.skills[i] = skill
			return skill, nil
		}
	}
	return skill, nil
}

func (m *memCatalogStore) DeleteSkill(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, sk := range m.skills {
		if sk.ID == id {
			m.skills = append(m.skills[:i], m.skills[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *memCatalogStore) SearchSkills(ctx context.Context, queryEmbedding []float32, topK int, agentID *uuid.UUID) ([]domain.Skill, error) {
	return nil, nil
}

func (m *memCatalogStore) CreateTechStack(ctx context.Context, stack domain.TechStack) (domain.TechStack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stack.ID == uuid.Nil {
		stack.ID = uuid.New()
	}
	m.stacks = append(m.stacks, stack)
	return stack, nil
}

func (m *memCatalogStore) GetTechStack(ctx context.Context, id uuid.UUID) (domain.TechStack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, st := range m.stacks {
		if st.ID == id {
			return st, nil
		}
	}
	return domain.TechStack{}, assert.AnError
}

func (m *memCatalogStore) ListTechStacksByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.TechStack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.TechStack
	for _, st := range m.stacks {
		if st.AgentID == agentID {
			out = append(out, st)
		}
	}
	return out, nil
}

func (m *memCatalogStore) UpdateTechStack(ctx context.Context, stack domain.TechStack) (domain.TechStack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, st := range m.stacks {
		if st.ID == stack.ID {
			m.stacks[i] = stack
			return stack, nil
		}
	}
	return domain.TechStack{}, assert.AnError
}

func (m *memCatalogStore) DeleteTechStack(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, st := range m.stacks {
		if st.ID == id {
			m.stacks = append(m.stacks[:i], m.stacks[i+1:]...)
			return nil
		}
	}
	return assert.AnError
}

func (m *memCatalogStore) CreateAgent(ctx context.Context, agent domain.Agent) (domain.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if agent.ID == uuid.Nil {
		agent.ID = uuid.New()
	}
	m.agents = append(m.agents, agent)
	return agent, nil
}

func (m *memCatalogStore) GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.agents {
		if a.ID == id {
			return a, nil
		}
	}
	return domain.Agent{}, assert.AnError
}

func (m *memCatalogStore) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Agent(nil), m.agents...), nil
}

func (m *memCatalogStore) UpdateAgent(ctx context.Context, agent domain.Agent) (domain.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, a := range m.agents {
		if a.ID == agent.ID {
			m.agents[i] = agent
			return agent, nil
		}
	}
	return domain.Agent{}, assert.AnError
}

func (m *memCatalogStore) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *memCatalogStore) CreateRule(ctx context.Context, rule domain.OrchestratorRule) (domain.OrchestratorRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rule.ID == uuid.Nil {
		rule.ID = uuid.New()
	}
	m.rules = append(m.rules, rule)
	return rule, nil
}

func (m *memCatalogStore) GetRule(ctx context.Context, id uuid.UUID) (domain.OrchestratorRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rules {
		if r.ID == id {
			return r, nil
		}
	}
	return domain.OrchestratorRule{}, assert.AnError
}

func (m *memCatalogStore) ListRules(ctx context.Context) ([]domain.OrchestratorRule, error) {
	return nil, nil
}

func (m *memCatalogStore) ListRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.OrchestratorRule
	for _, r := range m.rules {
		if r.AgentID == agentID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memCatalogStore) UpdateRule(ctx context.Context, rule domain.OrchestratorRule) (domain.OrchestratorRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rules {
		if r.ID == rule.ID {
			m.rules[i] = rule
			return rule, nil
		}
	}
	return rule, nil
}

func (m *memCatalogStore) DeleteRule(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *memCatalogStore) ListEnabledRules(ctx context.Context) ([]domain.OrchestratorRule, error) {
	return nil, nil
}

func (m *memCatalogStore) ListEnabledRulesByAgent(ctx context.Context, agentID uuid.UUID) ([]domain.OrchestratorRule, error) {
	return nil, nil
}

func (m *memCatalogStore) CreatePlan(ctx context.Context, plan domain.OrchestrationPlan, tasks []domain.PlanTask) (domain.OrchestrationPlan, error) {
	return plan, nil
}

func (m *memCatalogStore) GetPlanByRunID(ctx context.Context, runID uuid.UUID) (domain.PlanView, error) {
	return domain.PlanView{}, nil
}

func (m *memCatalogStore) UpdatePlanStatus(ctx context.Context, planID uuid.UUID, status string) error {
	return nil
}

func (m *memCatalogStore) UpdatePlanJSON(ctx context.Context, planID uuid.UUID, planJSON []byte) error {
	return nil
}

func (m *memCatalogStore) AppendPlanTasks(ctx context.Context, planID uuid.UUID, tasks []domain.PlanTask) ([]domain.PlanTask, error) {
	return tasks, nil
}

func (m *memCatalogStore) UpdateTaskStatus(ctx context.Context, taskID uuid.UUID, status, result, errMsg string) error {
	return nil
}

func (m *memCatalogStore) ListPlanTasks(ctx context.Context, planID uuid.UUID) ([]domain.PlanTask, error) {
	return nil, nil
}

type stubLLMClient struct{}

func (stubLLMClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (stubLLMClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (stubLLMClient) Models(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (stubLLMClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	return nil, nil
}

// claudeCodeAttached is the probe a host with the CLI wired up answers with.
func claudeCodeAttached(p domain.LLMProviderType) bool {
	return p == domain.LLMProviderClaudeCode
}

// seedBuiltinTemplate adds one built-in template for CreateAgentFromTemplate
// to consume. Templates are no longer boot-time seeded — they are rows the
// user saved, or freshly upserted from the catalog — so the tests wire them in
// directly and only exercise what CreateAgentFromTemplate does with them.
func seedBuiltinTemplate(t *testing.T, store *memTemplateStore, name string) domain.AgentTemplate {
	t.Helper()
	tpl, err := store.UpsertByName(context.Background(), domain.AgentTemplate{
		Name: name, Description: "built-in role", BuiltIn: true,
	})
	require.NoError(t, err)
	return tpl
}

// fillTemplateAgentModels derives the default model pair from the provider the
// new agent is being saved onto: Claude Code leaves both empty (the CLI knows
// its own defaults), an HTTP provider gets the two names its definition
// carries, and a template that names no provider at all gets Claude Code only
// when this host can actually run it.
func TestCreateAgentFromTemplate_ProviderDefaults(t *testing.T) {
	for _, tc := range []struct {
		name      string
		probe     func(domain.LLMProviderType) bool
		template  domain.AgentTemplate
		override  domain.CreateAgentRequest
		wantType  domain.LLMProviderType
		wantModel string
		wantHeavy string
	}{
		{
			name: "host can run Claude Code", probe: claudeCodeAttached,
			template:  domain.AgentTemplate{},
			wantType:  domain.LLMProviderClaudeCode,
			wantModel: "", wantHeavy: "",
		},
		{
			name:     "no CLI on the host",
			template: domain.AgentTemplate{},
			wantType: "", wantModel: "", wantHeavy: "",
		},
		{
			name:      "HTTP provider override gets its definitions models",
			template:  domain.AgentTemplate{},
			override:  domain.CreateAgentRequest{ProviderType: domain.LLMProviderAnthropic},
			wantType:  domain.LLMProviderAnthropic,
			wantModel: "claude-sonnet-5", wantHeavy: "claude-opus-5",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemCatalogStore()
			templates := &memTemplateStore{}
			svc := NewService(store, stubLLMClient{}, "")
			svc.SetTemplateStore(templates)
			if tc.probe != nil {
				svc.SetHostExecutorProbe(tc.probe)
			}
			tpl := seedBuiltinTemplate(t, templates, "system-architect")
			tpl.Model = tc.template.Model

			agent, err := svc.CreateAgentFromTemplate(context.Background(), tpl.ID, tc.override)
			require.NoError(t, err)
			assert.Equal(t, tc.wantType, agent.ProviderType)
			assert.Equal(t, tc.wantModel, agent.Model)
			assert.Equal(t, tc.wantHeavy, agent.ModelHeavy)
		})
	}
}

// A model an operator chose on create — the override, since a built-in
// template itself never names one — is left alone entirely: the helper fills
// the pair only when neither half is set.
func TestCreateAgentFromTemplate_LeavesAnOverriddenModelAlone(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetHostExecutorProbe(claudeCodeAttached)
	tpl := seedBuiltinTemplate(t, templates, "system-architect")

	agent, err := svc.CreateAgentFromTemplate(context.Background(), tpl.ID, domain.CreateAgentRequest{
		ProviderType: domain.LLMProviderAnthropic, Model: "custom-model",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.LLMProviderAnthropic, agent.ProviderType)
	assert.Equal(t, "custom-model", agent.Model)
	assert.Empty(t, agent.ModelHeavy, "a half-set pair is not topped up")
}

// Creating from a built-in template copies its KPIs onto the new agent the
// same way it carries skills and subscriptions.
func TestCreateAgentFromTemplate_CopiesDefaultKPIs(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	kpis := &memKPIStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetKPIStore(kpis)
	ctx := context.Background()
	tpl, err := templates.UpsertByName(ctx, domain.AgentTemplate{
		Name: "qa-agent", Description: "built-in role", BuiltIn: true,
		KPIs: []domain.CreateKPIRequest{{
			MetricKey: "qa_strictness", Name: "Strictness", Description: "d",
			Period: "month", TargetFull: 100, TargetHalf: 50, Weight: 1, Enabled: true,
		}},
	})
	require.NoError(t, err)

	agent, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{})
	require.NoError(t, err)

	got, err := kpis.ListByAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "qa_strictness", got[0].MetricKey)
	assert.Equal(t, agent.ID, got[0].AgentID)
}

// parseSeedDoc is the frontmatter reader shared by catalog files and the
// merge flow's model responses; the generator's round-trip test covered it
// against every real agent, this keeps the parser itself honest in CI.
func TestParseSeedDoc(t *testing.T) {
	meta, body, err := parseSeedDoc("---\nname: tally\ncategory: check\n---\n\nDo the sums.\n")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"name": "tally", "category": "check"}, meta)
	assert.Equal(t, "Do the sums.", body)

	for _, raw := range []string{"no opener", "---\nname: x\n", "---\ninvalid line\n---\nbody\n", "---\nname: x\n---\n\n"} {
		_, _, err := parseSeedDoc(raw)
		assert.Error(t, err, "input %q must fail to parse", raw)
	}
}

type memKPIStore struct {
	kpis []domain.AgentKPI
}

func (m *memKPIStore) CreateKPI(_ context.Context, k domain.AgentKPI) (domain.AgentKPI, error) {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	m.kpis = append(m.kpis, k)
	return k, nil
}

func (m *memKPIStore) UpdateKPI(_ context.Context, k domain.AgentKPI) (domain.AgentKPI, error) {
	for i, existing := range m.kpis {
		if existing.ID == k.ID {
			m.kpis[i] = k
			return k, nil
		}
	}
	return domain.AgentKPI{}, assert.AnError
}

func (m *memKPIStore) DeleteKPI(_ context.Context, id uuid.UUID) error {
	for i, k := range m.kpis {
		if k.ID == id {
			m.kpis = append(m.kpis[:i], m.kpis[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *memKPIStore) GetKPI(_ context.Context, id uuid.UUID) (domain.AgentKPI, error) {
	for _, k := range m.kpis {
		if k.ID == id {
			return k, nil
		}
	}
	return domain.AgentKPI{}, assert.AnError
}

func (m *memKPIStore) ListByAgent(_ context.Context, agentID uuid.UUID) ([]domain.AgentKPI, error) {
	var out []domain.AgentKPI
	for _, k := range m.kpis {
		if k.AgentID == agentID {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memKPIStore) UpsertResult(_ context.Context, r domain.AgentKPIResult) (domain.AgentKPIResult, error) {
	return r, nil
}

func (m *memKPIStore) ListResults(context.Context, uuid.UUID, time.Time, time.Time) ([]domain.AgentKPIResult, error) {
	return nil, nil
}

func (m *memKPIStore) LatestResults(context.Context, uuid.UUID) ([]domain.AgentKPIResult, error) {
	return nil, nil
}
