package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentSkillsCatalog answers the one lookup a subtask performs when it builds
// its skill index; the embedded interface leaves the rest unimplemented.
type agentSkillsCatalog struct {
	port.CatalogStore
	skills []domain.Skill
}

func (c agentSkillsCatalog) ListSkillsByAgent(_ context.Context, _ uuid.UUID) ([]domain.Skill, error) {
	return c.skills, nil
}

// A subtask used to know only about the skills the planner happened to name, so
// an agent configured with nineteen of them ran with three. The index is the
// agent's configuration, not the plan's guess.
func TestEnabledAgentSkills_IndexesEverythingTheOperatorEnabled(t *testing.T) {
	agentID := uuid.New()
	catalog := agentSkillsCatalog{skills: []domain.Skill{
		{ID: uuid.New(), AgentID: agentID, Name: "react-typescript-patterns", Enabled: true},
		{ID: uuid.New(), AgentID: agentID, Name: "routing-state", Enabled: true},
		{ID: uuid.New(), AgentID: agentID, Name: "retired-workflow", Enabled: false},
	}}

	skills, err := orchestrator.EnabledAgentSkillsForTest(context.Background(), catalog, agentID)

	require.NoError(t, err)
	require.Len(t, skills, 2, "a disabled skill must stay out of reach")
	assert.Equal(t, "react-typescript-patterns", skills[0].Name)
	assert.Equal(t, "routing-state", skills[1].Name)
}

// The run whose agent reported "we could not examine the project structure"
// while the repository sat one directory up: every code tool and shell command
// resolves to the subtask workspace, and that workspace was a freshly created
// empty folder inside the checkout.
func TestResolveSubtaskWorkspace_WorksInsideAnExistingCheckout(t *testing.T) {
	checkout := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(checkout, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(checkout, "index.html"), []byte("<html>"), 0o644))

	dir, err := orchestrator.ResolveSubtaskWorkspaceForTest(checkout, "PISH-1")

	require.NoError(t, err)
	assert.Equal(t, checkout, dir, "a prepared checkout is the workspace, not the parent of an empty one")
}

// Without a checkout the per-subtask scratch directory is still what isolation
// wants: chat orchestration runs on a bare workspace.
func TestResolveSubtaskWorkspace_IsolatesOnABareWorkspace(t *testing.T) {
	bare := t.TempDir()

	dir, err := orchestrator.ResolveSubtaskWorkspaceForTest(bare, "PISH-1")

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(bare, "PISH-1"), dir)
	info, statErr := os.Stat(dir)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestResolveSubtaskWorkspace_NoWorkspaceStaysEmpty(t *testing.T) {
	dir, err := orchestrator.ResolveSubtaskWorkspaceForTest("", "PISH-1")

	require.NoError(t, err)
	assert.Empty(t, dir)
}

func TestPlannedSkillFocus_NamesThePicksWithoutHidingTheRest(t *testing.T) {
	picked := uuid.New()
	skills := []domain.Skill{
		{ID: picked, Name: "routing-state", Enabled: true},
		{ID: uuid.New(), Name: "accessibility-basics", Enabled: true},
	}

	focus := orchestrator.PlannedSkillFocusForTest([]string{picked.String()}, skills)

	assert.Contains(t, focus, "routing-state")
	assert.Contains(t, focus, "your other skills still apply")
}

// The planner may name a skill that was since disabled or belongs to another
// agent. That is not worth failing a subtask over — it is simply not a pick.
func TestPlannedSkillFocus_IgnoresIdsTheAgentCannotLoad(t *testing.T) {
	skills := []domain.Skill{{ID: uuid.New(), Name: "routing-state", Enabled: true}}

	assert.Empty(t, orchestrator.PlannedSkillFocusForTest([]string{uuid.New().String()}, skills))
	assert.Empty(t, orchestrator.PlannedSkillFocusForTest(nil, skills))
}

func (c agentSkillsCatalog) ListTechStacksByAgent(_ context.Context, _ uuid.UUID) ([]domain.TechStack, error) {
	return nil, nil
}
