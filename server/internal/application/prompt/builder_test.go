package prompt_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSystemPrompt(t *testing.T) {
	agent := domain.Agent{
		ID:           uuid.New(),
		Name:         "backend",
		SystemPrompt: "You are a backend engineer.",
	}
	skills := []domain.Skill{
		{ID: uuid.New(), Name: "go-patterns", Description: "Go mimari desenleri", Content: "Use hexagonal architecture."},
	}
	rules := []string{"Always write tests."}

	result := prompt.BuildSystemPrompt(agent, skills, nil, rules, "tr")
	require.Contains(t, result, "You are a backend engineer.")
	require.Contains(t, result, "## Skills (index — load on demand)")
	require.Contains(t, result, "go-patterns — Go mimari desenleri")
	require.NotContains(t, result, "Use hexagonal architecture.", "skill content must be lazy-loaded via load_skill, not inlined")
	require.Contains(t, result, "load_skill")
	require.Contains(t, result, "Always write tests.")
	require.Contains(t, result, prompt.ToolSelectionGuidance())
	require.Contains(t, result, prompt.LanguageInstruction("tr"))
}

func TestSkillIndexMessage(t *testing.T) {
	assert.Empty(t, prompt.SkillIndexMessage(nil, nil, false))
	assert.Empty(t, prompt.SkillIndexMessage([]domain.Skill{{Name: "empty-content"}}, nil, false))
	idx := prompt.SkillIndexMessage([]domain.Skill{{Name: "a", Description: "desc", Content: "body"}}, nil, false)
	assert.Contains(t, idx, "- a — desc")
	assert.NotContains(t, idx, "body")
	assert.NotContains(t, idx, "create_skill", "without self-evolution the prompt must not promise create_skill")
	assert.NotContains(t, idx, "### General skills", "an index with no stacked skill stays a flat list")

	withCreate := prompt.SkillIndexMessage([]domain.Skill{{Name: "a", Description: "desc", Content: "body"}}, nil, true)
	assert.Contains(t, withCreate, "- a — desc")
	assert.Contains(t, withCreate, "create_skill")

	// An empty index still invites skill authoring when self-evolution is on.
	emptyWithCreate := prompt.SkillIndexMessage(nil, nil, true)
	assert.Contains(t, emptyWithCreate, "create_skill")
}

func TestSkillIndexMessageGroupsByTechStack(t *testing.T) {
	django := domain.TechStack{ID: uuid.New(), Name: "Django", Description: "DRF backend"}
	empty := domain.TechStack{ID: uuid.New(), Name: "Flutter"}
	gone := uuid.New()
	skills := []domain.Skill{
		{Name: "general-skill", Description: "everywhere", Content: "body"},
		{Name: "drf-viewsets", Description: "viewsets", Content: "body", TechStackID: &django.ID},
		{Name: "orphan", Description: "stack deleted", Content: "body", TechStackID: &gone},
	}

	idx := prompt.SkillIndexMessage(skills, []domain.TechStack{django, empty}, false)
	assert.Contains(t, idx, "### General skills")
	assert.Contains(t, idx, "### Django — DRF backend")
	assert.NotContains(t, idx, "Flutter", "a stack with no skill earns no heading")

	general := idx[strings.Index(idx, "### General skills"):strings.Index(idx, "### Django")]
	assert.Contains(t, general, "- general-skill — everywhere")
	assert.Contains(t, general, "- orphan — stack deleted", "a skill whose stack is gone falls back to general")
	assert.Contains(t, idx[strings.Index(idx, "### Django"):], "- drf-viewsets — viewsets")
}

func TestBuildSystemPrompt_CreateSkillHintFollowsFlagAndPolicy(t *testing.T) {
	skills := []domain.Skill{{Name: "a", Description: "desc", Content: "body"}}

	evolving := domain.Agent{
		SystemPrompt:         "You are a backend engineer.",
		SelfEvolutionEnabled: true,
		ToolPolicy:           domain.ToolPolicy{AllowTools: []string{"load_skill", "create_skill"}},
	}
	assert.Contains(t, prompt.BuildSystemPrompt(evolving, skills, nil, nil, ""), "create_skill")

	static := evolving
	static.SelfEvolutionEnabled = false
	assert.NotContains(t, prompt.BuildSystemPrompt(static, skills, nil, nil, ""), "create_skill")

	// Flag on but the stored policy never got the tool: no promise either.
	noTool := evolving
	noTool.ToolPolicy = domain.ToolPolicy{AllowTools: []string{"load_skill"}}
	assert.NotContains(t, prompt.BuildSystemPrompt(noTool, skills, nil, nil, ""), "create_skill")
}

func TestMemoryContextMessage(t *testing.T) {
	assert.Empty(t, prompt.MemoryContextMessage(nil, ""))
	agentID := uuid.New()
	repoID := uuid.New()
	msg := prompt.MemoryContextMessage([]domain.AgentMemory{
		{AgentID: agentID, Content: "User prefers Turkish", Category: "preference"},
		{AgentID: agentID, Content: "Never push to main"},
		{Content: "Build with make dev"}, // AgentID nil = team-shared
	}, "")
	assert.Contains(t, msg, "## Agent Memory")
	assert.Contains(t, msg, "[preference] User prefers Turkish")
	assert.Contains(t, msg, "- Never push to main")
	assert.Contains(t, msg, "[team] Build with make dev")
	assert.NotContains(t, msg, "### Project memory")

	scoped := prompt.MemoryContextMessage([]domain.AgentMemory{
		{AgentID: agentID, RepositoryID: &repoID, Content: "Verify with make check"},
		{RepositoryID: &repoID, Content: "Stage deploys on merge"},
		{AgentID: agentID, Content: "Never push to main"},
	}, "tasktrooper")
	assert.Contains(t, scoped, "### Project memory — tasktrooper (only valid in this repository)")
	assert.Contains(t, scoped, "- Verify with make check")
	assert.Contains(t, scoped, "- [team] Stage deploys on merge")
	assert.Contains(t, scoped, "### Global memory (valid across every repository)")
	// The global section must not swallow project lines: the split is what
	// stops one repository's build command being applied in another.
	globalPart := scoped[strings.Index(scoped, "### Global memory"):]
	assert.NotContains(t, globalPart, "make check")
}

func TestKPIContextMessage(t *testing.T) {
	assert.Empty(t, prompt.KPIContextMessage(nil, nil))
	kpiID := uuid.New()
	kpis := []domain.AgentKPI{
		{ID: kpiID, MetricKey: "revisions_received", Name: "Haftalık revizyon", Period: "weekly", TargetFull: 1, TargetHalf: 3, Enabled: true},
		{ID: uuid.New(), MetricKey: "disabled_metric", Enabled: false},
	}
	results := []domain.AgentKPIResult{{KPIID: kpiID, MeasuredValue: 2, Attainment: 0.5}}
	msg := prompt.KPIContextMessage(kpis, results)
	assert.Contains(t, msg, "## KPI Objectives")
	assert.Contains(t, msg, "Haftalık revizyon")
	assert.Contains(t, msg, "attainment 50%")
	assert.NotContains(t, msg, "disabled_metric")
}

func TestBuildSystemPrompt_EmptyParts(t *testing.T) {
	result := prompt.BuildSystemPrompt(domain.Agent{}, nil, nil, nil, "")
	assert.Contains(t, result, prompt.ToolSelectionGuidance())
	assert.Contains(t, result, "web_search")
}

func TestToolSelectionGuidance(t *testing.T) {
	g := prompt.ToolSelectionGuidance()
	assert.Contains(t, g, "web_search")
	assert.Contains(t, g, "run_terminal")
	assert.Contains(t, g, "who is")
}

// Every agent run carries the no-repeat contract, not just the ones the loop
// guard has already caught spinning. The guard is the backstop; the rule is
// what keeps a run from reaching it.
func TestBuildSystemPromptCarriesRepeatCallRule(t *testing.T) {
	result := prompt.BuildSystemPrompt(domain.Agent{}, nil, nil, nil, "")
	assert.Contains(t, result, prompt.RepeatCallGuidance())
}

func TestRepeatCallGuidance(t *testing.T) {
	g := prompt.RepeatCallGuidance()
	assert.Contains(t, g, "same arguments")
	// The failure this rule exists for: a silent success read as a failure.
	assert.Contains(t, g, "sed")
	assert.Contains(t, g, "Empty output")
	// Re-running a build after an edit is legitimate and must stay allowed.
	assert.Contains(t, g, "after an edit")
	assert.Contains(t, g, "never quote")
}

// A CLI run reaches TaskTrooper's tools over MCP, where ask_user is refused
// outright. Telling it to call ask_user names a tool it does not hold; telling
// it to keep questions out of its message body forbids the only channel it has.
func TestBuildSystemPromptFor_CLIRunGetsNoAskUserProtocol(t *testing.T) {
	agent := domain.Agent{SystemPrompt: "You are a backend engineer."}

	cli := prompt.BuildSystemPromptFor(agent, nil, nil, nil, "", prompt.SkillsOnDisk)
	assert.NotContains(t, cli, "ask_user")
	assert.NotContains(t, cli, "Do not write clarification questions in your message body")
	assert.Contains(t, cli, "closing message")
	assert.Contains(t, cli, "Never assume missing requirements")

	// The loop providers do hold ask_user, and keep the protocol.
	loop := prompt.BuildSystemPromptFor(agent, nil, nil, nil, "", prompt.SkillsInPrompt)
	assert.Contains(t, loop, "ask_user")
}

func TestLanguageInstruction(t *testing.T) {
	assert.Contains(t, prompt.LanguageInstruction("tr"), "Turkish")
	assert.Contains(t, prompt.LanguageInstruction("en"), "English")
}

func TestSubtaskWorkspaceNote(t *testing.T) {
	assert.Contains(t, prompt.SubtaskWorkspaceNote("/tmp/ws"), "INTERNAL")
	assert.Contains(t, prompt.SubtaskWorkspaceNote("/tmp/ws"), "/tmp/ws")
}

func TestUserFacingGuidance(t *testing.T) {
	assert.Contains(t, prompt.UserFacingGuidance(), "greetings")
}

func TestKPIContextExplainsCleanOnlyMeasurement(t *testing.T) {
	msg := prompt.KPIContextMessage([]domain.AgentKPI{{
		ID: uuid.New(), MetricKey: "clean_time_in_progress", Name: "Clean cycle time",
		Period: domain.KPIPeriodWeekly, TargetFull: 6, TargetHalf: 16, Weight: 1, Enabled: true,
	}}, nil)

	// Without this an agent reads a lower-better time target as "go faster"
	// and pays for it out of quality.
	require.Contains(t, msg, "without a revision")
	require.Contains(t, msg, "cannot buy speed with quality")
}
