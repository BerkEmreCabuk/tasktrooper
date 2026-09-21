package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The catalog is the source of truth for who holds which tools now. The
// builder functions below are the code's own contract for those lists; this
// test pins the two to each other, so a catalog rewrite cannot silently
// remove a tool the merge/rollback/deploy invariants depend on (see
// role_tools_qa_test.go) and a code edit cannot drift ahead of the shipped
// catalog.
func TestCatalogRoleToolPolicies(t *testing.T) {
	catalog := repoCatalogAgents(t)

	byName := map[string]string{
		"backend-developer":  "developerToolPolicy",
		"frontend-developer": "developerToolPolicy",
		"mobile-developer":   "mobileDeveloperToolPolicy",
		"product-manager":    "productManagerToolPolicy",
		"qa-agent":           "qaToolPolicy",
		"system-architect":   "architectToolPolicy",
	}
	expected := map[string]domain.ToolPolicy{
		"backend-developer":  developerToolPolicy(),
		"frontend-developer": developerToolPolicy(),
		"mobile-developer":   mobileDeveloperToolPolicy(),
		"product-manager":    productManagerToolPolicy(),
		"qa-agent":           qaToolPolicy(),
		"system-architect":   architectToolPolicy(),
	}
	for slug, want := range expected {
		agent, ok := catalog[slug]
		require.Truef(t, ok, "catalog is missing agent %q", slug)
		assert.Truef(t, toolPolicyEqual(agent.ToolPolicy, want),
			"%s tool policy drifted from %s", slug, byName[slug])
	}

	// The one place a developer policy differs: the device tools belong to the
	// mobile developer alone. There is one physical phone, and putting three
	// roles in the queue for it only makes them wait on hardware two of them
	// cannot use.
	dev := expected["backend-developer"]
	mob := expected["mobile-developer"]
	assert.NotContains(t, dev.AllowTools, "mobile_tap")
	assert.Contains(t, mob.AllowTools, "mobile_tap")

	// QA and PM test the app on the device too — QA runs the round, PM signs
	// off on it in UAT.
	qa := expected["qa-agent"]
	assert.Contains(t, qa.AllowTools, "mobile_launch_app")
	assert.Contains(t, qa.AllowTools, "mobile_screenshot")
	assert.Contains(t, expected["product-manager"].AllowTools, "mobile_screenshot")
}

func TestToolPolicyEqual(t *testing.T) {
	a := productManagerToolPolicy()
	b := productManagerToolPolicy()
	assert.True(t, toolPolicyEqual(a, b))
	b.AllowTools = append(b.AllowTools, "run_terminal")
	assert.False(t, toolPolicyEqual(a, b))
}
