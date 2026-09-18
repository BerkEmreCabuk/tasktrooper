package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// The disabled flag reaches a NEW install through the built-in template the
// same way any other skill field does — EnsureRoleTemplates upserts the whole
// skill list, this one included, with Enabled: false. There is no longer a
// reconcile step that could re-propagate a later flip to an agent someone
// already created; TestQAAutomationSetIsParkedNotRemoved above is what
// guards the source of truth (the role definition) staying parked.
