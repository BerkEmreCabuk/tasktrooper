package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQAAutomationSetIsParkedNotRemoved(t *testing.T) {
	catalog := repoCatalogAgents(t)
	qa, ok := catalog["qa-agent"]
	require.True(t, ok, "catalog is missing qa-agent")

	parkedSkills := map[string]bool{
		"e2e-automation-project":          true,
		"automation-pipeline-integration": true,
		"test-doubles-wiremock":           true,
		"test-database-seeding":           true,
	}
	seen := 0
	byName := map[string]bool{}
	for _, sk := range qa.Skills {
		byName[sk.Name] = true
		if parkedSkills[sk.Name] {
			seen++
			assert.False(t, sk.Enabled, "skill %s must stay parked disabled", sk.Name)
			assert.NotEmpty(t, sk.Content, "skill %s must keep its content for the next iteration", sk.Name)
		}
	}
	assert.Equal(t, len(parkedSkills), seen, "every parked automation skill must still be owned by the role")

	parkedRules := map[string]bool{"e2e-automation-project": true, "deterministic-test-env": true}
	rulesSeen := 0
	for _, r := range qa.Rules {
		if parkedRules[r.Name] {
			rulesSeen++
			assert.False(t, r.Enabled, "rule %s must stay parked disabled", r.Name)
		}
	}
	assert.Equal(t, len(parkedRules), rulesSeen)

	for _, name := range []string{"backend-manual-testing", "frontend-manual-testing", "mobile-manual-testing"} {
		assert.True(t, byName[name], "%s must still be owned by the role", name)
	}
}
