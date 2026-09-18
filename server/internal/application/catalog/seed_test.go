package catalog

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoleAgentDefinitions_CountAndSkills(t *testing.T) {
	defs := roleAgentDefinitions()
	assert.Len(t, defs, 6)
	names := map[string]bool{}
	for _, d := range defs {
		names[d.agent.Name] = true
		assert.GreaterOrEqual(t, len(d.skills), 6)
		assert.GreaterOrEqual(t, len(d.rules), 2)
		assert.True(t, d.agent.Enabled)
	}
	assert.True(t, names["system-architect"])
	assert.True(t, names["backend-developer"])
	assert.True(t, names["frontend-developer"])
	assert.True(t, names["mobile-developer"])
	assert.True(t, names["product-manager"])
	assert.True(t, names["qa-agent"])
}

func TestSeedData_AllFilesParseAndAreReferenced(t *testing.T) {
	referenced := map[string]bool{}
	for _, def := range roleAgentDefinitions() {
		require.NotEmpty(t, def.agent.SystemPrompt, def.agent.Name)
		require.NotEmpty(t, def.agent.Description, def.agent.Name)
		referenced["seeddata/agents/"+def.agent.Name+".md"] = true
		for _, sk := range def.skills {
			require.NotEmpty(t, sk.req.Name)
			require.NotEmpty(t, sk.req.Category, sk.req.Name)
			require.NotEmpty(t, sk.req.Description, sk.req.Name)
			require.NotEmpty(t, sk.req.Content, sk.req.Name)
		}
	}

	err := fs.WalkDir(seedData, "seeddata", func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		// The catalog is its own repository now, so it carries documentation of
		// its own. Those files describe the seeds rather than being one, and
		// holding them to a seed's frontmatter would fail the build for a
		// README that is doing its job.
		if !isSeedDoc(path) {
			return nil
		}
		require.True(t, strings.HasSuffix(path, ".md"), "unexpected seed file %s", path)
		raw, readErr := seedData.ReadFile(path)
		require.NoError(t, readErr, path)
		meta, body, parseErr := parseSeedDoc(string(raw))
		require.NoError(t, parseErr, path)
		require.NotEmpty(t, body, path)
		// A skill is a directory holding SKILL.md, so its name is the
		// directory's; an agent is still one file named after the role.
		base := strings.TrimSuffix(filepath.Base(path), ".md")
		if base == "SKILL" {
			base = filepath.Base(filepath.Dir(path))
		}
		require.Equal(t, base, meta["name"], path)
		require.NotContains(t, strings.ToLower(string(raw)), "superpowers", path)
		if strings.HasPrefix(path, "seeddata/agents/") {
			require.True(t, referenced[path], "agent %s is not used by any role agent", path)
		}
		return nil
	})
	require.NoError(t, err)
}

// isSeedDoc reports whether an embedded path is a seed the loader reads, as
// opposed to the repository's own documentation.
//
// Stated as a positive list of the two directories that hold seeds, rather than
// as an exclusion of README.md. The catalog is a repository people are meant to
// read and contribute to, so it will grow a docs/ or a LICENSE eventually, and
// a rule that named the one file we happen to have today would fail the build
// on the next one.
func isSeedDoc(path string) bool {
	return strings.HasPrefix(path, "seeddata/agents/") || strings.HasPrefix(path, "seeddata/skills/")
}

func TestSeedData_EverySkillFileIsReferenced(t *testing.T) {
	used := map[string]int{}
	for _, def := range roleAgentDefinitions() {
		for _, sk := range def.skills {
			used[sk.req.Name]++
		}
	}
	err := fs.WalkDir(seedData, "seeddata/skills", func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		// A skill is a DIRECTORY holding SKILL.md, so the skill's name is the
		// directory's. Taking it from the file would compare every skill in the
		// tree against the literal "SKILL".
		name := filepath.Base(filepath.Dir(path))
		require.Positive(t, used[name], "skill file %s is not referenced by any role agent", path)
		return nil
	})
	require.NoError(t, err)
}

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
	return domain.OrchestratorRule{}, nil
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

// Boot no longer creates any agent — only EnsureRoleTemplates runs, which
// upserts the six built-in templates and nothing in the agents table. This is
// the decision this file used to exercise through EnsureRoleAgents at boot;
// there is no boot-time agent creation left to test.
func TestEnsureRoleTemplates_CreatesNoAgents(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetHostExecutorProbe(claudeCodeAttached)

	require.NoError(t, svc.EnsureRoleTemplates(context.Background()))

	agents, err := store.ListAgents(context.Background())
	require.NoError(t, err)
	assert.Empty(t, agents)
	assert.Len(t, templates.templates, len(roleAgentDefinitions()))
	for _, tpl := range templates.templates {
		assert.True(t, tpl.BuiltIn, tpl.Name)
	}
}

// Creating a role agent from its built-in template does what the old
// boot-time seed used to do on CREATE: fill the Claude Code provider/model
// pair when the host can actually run it.
func TestCreateAgentFromTemplate_FillsClaudeCodeModelsWhenHostCanRun(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetHostExecutorProbe(claudeCodeAttached)
	ctx := context.Background()
	require.NoError(t, svc.EnsureRoleTemplates(ctx))

	for _, tpl := range templates.templates {
		agent, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{})
		require.NoError(t, err)
		// The provider is asserted with the names, not beside them: these two
		// aliases are Claude Code CLI values and mean nothing anywhere else.
		assert.Equal(t, domain.LLMProviderClaudeCode, agent.ProviderType, agent.Name)
		assert.Equal(t, "sonnet", agent.Model, agent.Name)
		assert.Equal(t, "opus", agent.ModelHeavy, agent.Name)
	}
}

// A host with no CLI attached gets no model names at all. CreateAgent refuses
// an agent on a provider it cannot execute, so filling the pair there would
// not produce a mildly wrong agent — it would produce no agent at all.
func TestCreateAgentFromTemplate_NoModelsWhenHostCannotRun(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	ctx := context.Background()
	require.NoError(t, svc.EnsureRoleTemplates(ctx))

	for _, tpl := range templates.templates {
		agent, err := svc.CreateAgentFromTemplate(ctx, tpl.ID, domain.CreateAgentRequest{})
		require.NoError(t, err)
		assert.Empty(t, agent.ProviderType, agent.Name)
		assert.Empty(t, agent.Model, agent.Name)
		assert.Empty(t, agent.ModelHeavy, agent.Name)
	}
}

// A provider the caller chose on create — the override, since a built-in
// template itself never names one — is left alone entirely. `sonnet` is a CLI
// alias; sending it to api.anthropic.com is the "Invalid model" failure
// TestUpdateAgent_ProviderSwitchDropsTheOldProvidersModels records.
func TestCreateAgentFromTemplate_LeavesAnOverriddenProviderAlone(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetHostExecutorProbe(claudeCodeAttached)
	ctx := context.Background()
	require.NoError(t, svc.EnsureRoleTemplates(ctx))

	agent, err := svc.CreateAgentFromTemplate(ctx, findTemplateID(t, templates, "system-architect"), domain.CreateAgentRequest{
		ProviderType: domain.LLMProviderAnthropic,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.LLMProviderAnthropic, agent.ProviderType)
	assert.Empty(t, agent.Model)
	assert.Empty(t, agent.ModelHeavy)
}

// Creating from the built-in qa-agent template also gets its default KPIs —
// the template carries defaultRoleKPIs the same way the old boot seed did.
func TestCreateAgentFromTemplate_SetsDefaultKPIs(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	kpis := &memKPIStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	svc.SetKPIStore(kpis)
	ctx := context.Background()
	require.NoError(t, svc.EnsureRoleTemplates(ctx))

	agent, err := svc.CreateAgentFromTemplate(ctx, findTemplateID(t, templates, "qa-agent"), domain.CreateAgentRequest{})
	require.NoError(t, err)

	got, err := kpis.ListByAgent(ctx, agent.ID)
	require.NoError(t, err)
	want := defaultRoleKPIs("qa-agent")
	require.Len(t, got, len(want))
	gotKeys := make(map[string]bool, len(got))
	for _, k := range got {
		gotKeys[k.MetricKey] = true
	}
	for _, w := range want {
		assert.True(t, gotKeys[w.MetricKey], "missing KPI %s", w.MetricKey)
	}
}

// EnsureRoleTemplates runs again on every boot (it upserts the built-in
// templates by name); an agent already created from one, and since edited by
// a user (or by self-evolution), must not be touched by that re-run — nothing
// reconciles an existing agent against its template any more.
func TestEnsureRoleTemplates_DoesNotRevertAUserEditedAgentSkill(t *testing.T) {
	store := newMemCatalogStore()
	templates := &memTemplateStore{}
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetTemplateStore(templates)
	ctx := context.Background()
	require.NoError(t, svc.EnsureRoleTemplates(ctx))

	agent, err := svc.CreateAgentFromTemplate(ctx, findTemplateID(t, templates, "qa-agent"), domain.CreateAgentRequest{})
	require.NoError(t, err)
	skills, err := svc.ListSkillsByAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.NotEmpty(t, skills)
	edited := skills[0]
	edited.Content = "an operator rewrote this skill by hand"
	_, err = store.UpdateSkill(ctx, edited)
	require.NoError(t, err)

	// A second boot re-upserts the templates from the same role definitions.
	require.NoError(t, svc.EnsureRoleTemplates(ctx))

	got, err := svc.GetSkillForAgent(ctx, agent.ID, edited.ID)
	require.NoError(t, err)
	assert.Equal(t, "an operator rewrote this skill by hand", got.Content)
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

// Every seeded name is one the CLI was actually probed with, so the picker the
// admin UI serves and the value the seed writes cannot drift apart.
func TestSeededModelsAreOfferedByTheClaudeCodePicker(t *testing.T) {
	offered := map[string]bool{}
	for _, opt := range domain.ClaudeCodeModels() {
		offered[opt.ID] = true
	}
	assert.True(t, offered[roleAgentModel], "seeded model %q is not a probed Claude Code alias", roleAgentModel)
	assert.True(t, offered[roleAgentModelHeavy], "seeded heavy model %q is not a probed Claude Code alias", roleAgentModelHeavy)
}
