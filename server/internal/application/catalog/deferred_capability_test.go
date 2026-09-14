package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The automation set is DEFERRED, not deleted: the QA agent keeps owning every
// one of those skills and rules, and none of them reaches its prompt — the
// builder injects enabled entries only. Deleting them instead would have made
// the next iteration a restoration job.
func TestQAAutomationSetIsParkedNotRemoved(t *testing.T) {
	var qa roleAgentDef
	for _, def := range roleAgentDefinitions() {
		if def.agent.Name == "qa-agent" {
			qa = def
		}
	}
	require.Equal(t, "qa-agent", qa.agent.Name)

	parkedSkills := map[string]bool{
		"e2e-automation-project":          true,
		"automation-pipeline-integration": true,
		"test-doubles-wiremock":           true,
		"test-database-seeding":           true,
	}
	seen := 0
	for _, sk := range qa.skills {
		if parkedSkills[sk.req.Name] {
			seen++
			assert.False(t, sk.req.Enabled, "skill %s must be seeded disabled this iteration", sk.req.Name)
			assert.NotEmpty(t, sk.req.Content, "skill %s must keep its content for the next iteration", sk.req.Name)
		}
	}
	assert.Equal(t, len(parkedSkills), seen, "every parked automation skill must still be owned by the role")

	parkedRules := map[string]bool{"e2e-automation-project": true, "deterministic-test-env": true}
	rulesSeen := 0
	for _, r := range qa.rules {
		if parkedRules[r.Name] {
			rulesSeen++
			assert.False(t, r.Enabled, "rule %s must be seeded disabled this iteration", r.Name)
		}
	}
	assert.Equal(t, len(parkedRules), rulesSeen)

	// The manual round is what stays on, for all three task kinds.
	manual := map[string]bool{}
	for _, sk := range qa.skills {
		if sk.req.Enabled {
			manual[sk.req.Name] = true
		}
	}
	for _, name := range []string{"backend-manual-testing", "frontend-manual-testing", "mobile-manual-testing"} {
		assert.True(t, manual[name], "%s must stay enabled", name)
	}
}

// Parking only works if it reaches installs that were seeded while the
// capability was still on — a flag the reconciler ignores is a flag that only
// applies to fresh databases.
func TestEnsureRoleAgentsPropagatesTheDisabledFlag(t *testing.T) {
	store := newMemCatalogStore()
	svc := NewService(store, stubLLMClient{}, "")

	agentID := uuid.New()
	var qa roleAgentDef
	for _, def := range roleAgentDefinitions() {
		if def.agent.Name == "qa-agent" {
			qa = def
		}
	}
	store.agents = []domain.Agent{{
		ID:           agentID,
		Name:         qa.agent.Name,
		SubagentType: qa.agent.SubagentType,
		SystemPrompt: qa.agent.SystemPrompt,
		ToolPolicy:   qa.agent.ToolPolicy,
		Enabled:      true,
	}}
	// Seeded by an older build, with the automation skill and rule switched on
	// and their text already current — so nothing but the flag differs.
	for _, sk := range qa.skills {
		if sk.req.Name == "e2e-automation-project" {
			store.skills = append(store.skills, domain.Skill{
				ID: uuid.New(), AgentID: agentID, Name: sk.req.Name, Category: sk.req.Category,
				Description: sk.req.Description, Content: sk.req.Content, Enabled: true,
			})
		}
	}
	for _, r := range qa.rules {
		if r.Name == "deterministic-test-env" {
			store.rules = append(store.rules, domain.OrchestratorRule{
				ID: uuid.New(), AgentID: agentID, Name: r.Name,
				Content: r.Content, Priority: r.Priority, Enabled: true,
			})
		}
	}

	require.NoError(t, svc.EnsureRoleAgents(context.Background()))

	skills, err := store.ListSkillsByAgent(context.Background(), agentID)
	require.NoError(t, err)
	for _, sk := range skills {
		if sk.Name == "e2e-automation-project" {
			assert.False(t, sk.Enabled, "an already-seeded automation skill must be switched off by the reconcile")
		}
	}
	rules, err := store.ListRulesByAgent(context.Background(), agentID)
	require.NoError(t, err)
	for _, r := range rules {
		if r.Name == "deterministic-test-env" {
			assert.False(t, r.Enabled, "an already-seeded automation rule must be switched off by the reconcile")
		}
	}
}
