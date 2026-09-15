package catalog

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

func TestEnsureRoleAgents_PreservesCustomToolPolicy(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")

	agentID := uuid.New()
	customPolicy := domain.ToolPolicy{
		AllowMCPServers: []string{"filesystem"},
		AllowTools:      []string{"run_terminal"},
	}
	store.agents = []domain.Agent{{
		ID:           agentID,
		Name:         "backend-developer",
		SubagentType: "backend-engineer",
		SystemPrompt: roleAgentDefinitions()[0].agent.SystemPrompt,
		ToolPolicy:   customPolicy,
		Enabled:      true,
	}}

	err := svc.EnsureRoleAgents(context.Background())
	require.NoError(t, err)

	got, err := store.GetAgent(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, customPolicy, got.ToolPolicy)
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

func TestEnsureRoleAgents_SeedsSonnetWithOpusForHardWork(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetHostExecutorProbe(claudeCodeAttached)

	require.NoError(t, svc.EnsureRoleAgents(context.Background()))

	agents, err := store.ListAgents(context.Background())
	require.NoError(t, err)
	require.Len(t, agents, 6)
	for _, a := range agents {
		// The provider is asserted with the names, not beside them: these two
		// aliases are Claude Code CLI values and mean nothing anywhere else.
		assert.Equal(t, domain.LLMProviderClaudeCode, a.ProviderType, a.Name)
		assert.Equal(t, "sonnet", a.Model, a.Name)
		assert.Equal(t, "opus", a.ModelHeavy, a.Name)
	}
}

// A host with no CLI attached gets no model names at all. CreateAgent refuses
// an agent on a provider it cannot execute, so seeding the pair there would not
// produce a mildly wrong agent — it would produce none of the six.
func TestEnsureRoleAgents_SeedsNoModelsWhereTheCLICannotRun(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")

	require.NoError(t, svc.EnsureRoleAgents(context.Background()))

	agents, err := store.ListAgents(context.Background())
	require.NoError(t, err)
	require.Len(t, agents, 6)
	for _, a := range agents {
		assert.Empty(t, a.ProviderType, a.Name)
		assert.Empty(t, a.Model, a.Name)
		assert.Empty(t, a.ModelHeavy, a.Name)
	}
}

// Seeding runs once, on first sight, so an install seeded before these
// defaults existed has role agents with empty models. It is reconciled on the
// next run rather than by a migration.
func TestEnsureRoleAgents_BackfillsAnAlreadySeededAgent(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetHostExecutorProbe(claudeCodeAttached)

	agentID := uuid.New()
	store.agents = []domain.Agent{{
		ID: agentID, Name: "backend-developer", SubagentType: "backend-engineer", Enabled: true,
	}}

	require.NoError(t, svc.EnsureRoleAgents(context.Background()))

	got, err := store.GetAgent(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, domain.LLMProviderClaudeCode, got.ProviderType)
	assert.Equal(t, "sonnet", got.Model)
	assert.Equal(t, "opus", got.ModelHeavy)
}

// A model a person picked survives the next seed run, and so does the empty
// ModelHeavy beside it: filling only the heavy half would bolt an opus
// escalation onto a choice made to stay cheap.
func TestEnsureRoleAgents_KeepsAModelAPersonPicked(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetHostExecutorProbe(claudeCodeAttached)

	agentID := uuid.New()
	store.agents = []domain.Agent{{
		ID: agentID, Name: "qa-agent", SubagentType: "qa-engineer", Enabled: true,
		ProviderType: domain.LLMProviderClaudeCode, Model: "haiku",
	}}

	require.NoError(t, svc.EnsureRoleAgents(context.Background()))

	got, err := store.GetAgent(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, "haiku", got.Model)
	assert.Empty(t, got.ModelHeavy)
}

// An agent somebody moved to an HTTP provider is left alone entirely. `sonnet`
// is a CLI alias; sending it to api.anthropic.com is the "Invalid model"
// failure TestUpdateAgent_ProviderSwitchDropsTheOldProvidersModels records.
func TestEnsureRoleAgents_LeavesAnotherProvidersAgentAlone(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")
	svc.SetHostExecutorProbe(claudeCodeAttached)

	agentID := uuid.New()
	store.agents = []domain.Agent{{
		ID: agentID, Name: "system-architect", SubagentType: "architect", Enabled: true,
		ProviderType: domain.LLMProviderAnthropic,
	}}

	require.NoError(t, svc.EnsureRoleAgents(context.Background()))

	got, err := store.GetAgent(context.Background(), agentID)
	require.NoError(t, err)
	assert.Equal(t, domain.LLMProviderAnthropic, got.ProviderType)
	assert.Empty(t, got.Model)
	assert.Empty(t, got.ModelHeavy)
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
