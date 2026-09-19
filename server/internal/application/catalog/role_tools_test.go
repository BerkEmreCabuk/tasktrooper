package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoleToolPolicies(t *testing.T) {
	defs := roleAgentDefinitions()
	byName := make(map[string]roleAgentDef, len(defs))
	for _, d := range defs {
		byName[d.agent.Name] = d
	}

	dev := byName["backend-developer"].agent.ToolPolicy
	assert.Contains(t, dev.AllowTools, "run_terminal")
	assert.Contains(t, dev.AllowTools, "web_search")
	assert.Contains(t, dev.AllowTools, "list_board_tasks")
	assert.Contains(t, dev.AllowTools, "move_board_task")
	assert.NotContains(t, dev.AllowTools, "create_board_task")

	pm := byName["product-manager"].agent.ToolPolicy
	assert.Contains(t, pm.AllowTools, "create_board_task")
	assert.Contains(t, pm.AllowTools, "list_board_tasks")
	assert.Contains(t, pm.AllowTools, "web_search")
	assert.Contains(t, pm.AllowTools, "grep_code")
	assert.Contains(t, pm.AllowTools, "browser_navigate")
	assert.Contains(t, pm.AllowTools, "browser_screenshot")
	assert.Contains(t, pm.AllowTools, "get_deploy_target")
	assert.Contains(t, pm.AllowTools, "update_deploy_target")
	assert.NotContains(t, pm.AllowTools, "run_terminal")

	qa := byName["qa-agent"].agent.ToolPolicy
	assert.Contains(t, qa.AllowTools, "list_board_tasks")
	assert.Contains(t, qa.AllowTools, "move_board_task")
	assert.Contains(t, qa.AllowTools, "run_terminal")
	assert.Contains(t, qa.AllowTools, "grep_code")
	assert.Contains(t, qa.AllowTools, "browser_navigate")
	assert.Contains(t, qa.AllowTools, "browser_wait_for")
	assert.Contains(t, qa.AllowTools, "update_deploy_target")
	assert.NotContains(t, qa.AllowTools, "create_board_task")
	assert.NotContains(t, qa.AllowTools, "claim_board_task")

	require.Equal(t, dev.AllowTools, byName["frontend-developer"].agent.ToolPolicy.AllowTools)

	// The mobile developer is the one developer role that is NOT the shared
	// policy: it also holds the device tools. The other two must not, because
	// there is one physical phone and putting three roles in the queue for it
	// only makes them wait on hardware two of them cannot use.
	mobileDev := byName["mobile-developer"].agent.ToolPolicy
	require.Equal(t, append(append([]string{}, dev.AllowTools...), roleMobileTools...), mobileDev.AllowTools)
	assert.NotContains(t, dev.AllowTools, "mobile_tap")
	assert.NotContains(t, byName["frontend-developer"].agent.ToolPolicy.AllowTools, "mobile_tap")

	// QA and PM test the app on the device too — QA runs the round, PM signs
	// off on it in UAT.
	assert.Contains(t, qa.AllowTools, "mobile_launch_app")
	assert.Contains(t, qa.AllowTools, "mobile_screenshot")
	assert.Contains(t, pm.AllowTools, "mobile_screenshot")
}

// The DevOps engineer is an implementer with the deploy surface: it writes
// files and runs commands like a developer, reads and acts on deploys and
// incidents, and holds neither of the two production writes that belong to QA.
func TestDevopsEngineerToolPolicy(t *testing.T) {
	defs := roleAgentDefinitions()
	byName := make(map[string]roleAgentDef, len(defs))
	for _, d := range defs {
		byName[d.agent.Name] = d
	}
	devops := byName["devops-engineer"].agent.ToolPolicy
	require.NotEmpty(t, devops.AllowTools, "an empty allowlist is unrestricted")

	for _, tool := range []string{
		"run_terminal", "write_file", "edit_file", "web_search", "grep_code",
		"list_board_tasks", "move_board_task", "claim_board_task",
		"get_pipeline_status", "get_deploy_target", "update_deploy_target",
		"list_deploy_templates", "load_deploy_template", "record_local_deploy",
		"get_task_deploy_status", "get_deploy_logs",
		"list_incidents", "get_incident", "propose_incident_remedy", "resolve_incident",
		"get_task_pull_request", "comment_on_pull_request", "commit_task_changes",
	} {
		assert.Contains(t, devops.AllowTools, tool)
	}

	// QA's, and only QA's: the role that last exercised the built product
	// lands the change and undoes it (migrations 104/105).
	assert.NotContains(t, devops.AllowTools, "merge_task_pull_request")
	assert.NotContains(t, devops.AllowTools, "rollback_task_release")
	// The PM owns the backlog.
	assert.NotContains(t, devops.AllowTools, "create_board_task")
	assert.NotContains(t, devops.AllowTools, "delete_board_task")
	// This role verifies by running the build and reading the deploy.
	assert.NotContains(t, devops.AllowTools, "browser_navigate")
	assert.NotContains(t, devops.AllowTools, "mobile_tap")
}

func TestToolPolicyEqual(t *testing.T) {
	a := productManagerToolPolicy()
	b := productManagerToolPolicy()
	assert.True(t, toolPolicyEqual(a, b))
	b.AllowTools = append(b.AllowTools, "run_terminal")
	assert.False(t, toolPolicyEqual(a, b))
}
