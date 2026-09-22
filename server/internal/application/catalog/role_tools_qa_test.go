package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Real migration-143 fixture rather than a hand-built stage, so these tests pin the exact behaviour combinations it seeds.
func stageFor(t *testing.T, taskType domain.TaskType, column domain.TaskColumn) (domain.WorkflowStage, domain.TaskTypeDef) {
	t.Helper()
	wf, ok := workflowtest.Default().Workflows[taskType]
	if !ok {
		t.Fatalf("workflowtest.Default() has no workflow for type %q", taskType)
	}
	stage, ok := wf.Stage(column)
	if !ok {
		t.Fatalf("workflow %q has no stage for column %s", taskType, column)
	}
	return stage, wf.Type
}

// Mirrors board.Runner's upliftedPolicy (runner.go) so the tests exercise the real pipeline.
func qaRunPolicyIn(t *testing.T, column domain.TaskColumn) domain.ToolPolicy {
	t.Helper()
	stage, typeDef := stageFor(t, "task", column)
	return domain.RestrictToolsForStage(
		domain.UpliftWorkspaceTools(
			domain.MergeToolPolicy(domain.ToolPolicy{}, qaToolPolicy()),
		),
		stage, typeDef,
	)
}

func TestQARunPolicyKeepsTheVerdictTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA,
	} {
		t.Run(string(column), func(t *testing.T) {
			policy := qaRunPolicyIn(t, column)
			require.NotEmpty(t, policy.AllowTools)

			allowed := func(name string) bool { return domain.ToolAllowedByPolicy(name, policy) }
			assert.True(t, allowed("list_acceptance_criteria"))
			assert.True(t, allowed("review_criterion"))
			assert.True(t, allowed("move_board_task"))
			assert.True(t, allowed("add_task_comment"))
			assert.True(t, allowed("run_terminal"))

			assert.False(t, allowed("write_file"))
			assert.False(t, allowed("edit_file"))
			assert.False(t, allowed("commit_task_changes"))
			assert.False(t, allowed(domain.MergePullRequestToolName))
		})
	}
}

func TestQARunPolicyKeepsTheMergeToolInDone(t *testing.T) {
	policy := qaRunPolicyIn(t, domain.TaskColumnDone)
	require.NotEmpty(t, policy.AllowTools)

	assert.True(t, domain.ToolAllowedByPolicy(domain.MergePullRequestToolName, policy))
	assert.True(t, domain.ToolAllowedByPolicy("get_task_pull_request", policy))
	assert.True(t, domain.ToolAllowedByPolicy("get_pipeline_status", policy))
	assert.True(t, domain.ToolAllowedByPolicy("add_task_comment", policy))
	assert.False(t, domain.ToolAllowedByPolicy("commit_task_changes", policy))
	assert.False(t, domain.ToolAllowedByPolicy("write_file", policy))
	assert.False(t, domain.ToolAllowedByPolicy("edit_file", policy))
}

func TestOnlyQAHoldsTheMergeTool(t *testing.T) {
	for name, policy := range map[string]domain.ToolPolicy{
		"developer":        developerToolPolicy(),
		"mobile-developer": mobileDeveloperToolPolicy(),
		"system-architect": architectToolPolicy(),
		"product-manager":  productManagerToolPolicy(),
	} {
		t.Run(name, func(t *testing.T) {
			for _, tool := range policy.AllowTools {
				assert.NotEqual(t, domain.MergePullRequestToolName, tool)
			}
		})
	}
	assert.Contains(t, qaToolPolicy().AllowTools, domain.MergePullRequestToolName)
}

func TestWorkspaceUpliftDoesNotGrantTheMergeTool(t *testing.T) {
	uplifted := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"run_terminal"}})

	assert.False(t, domain.ToolAllowedByPolicy(domain.MergePullRequestToolName, uplifted))
	assert.True(t, domain.ToolAllowedByPolicy("commit_task_changes", uplifted))
}

func TestQAHoldsTheDeployWatchTools(t *testing.T) {
	policy := qaToolPolicy()
	for _, name := range []string{
		domain.DeployStatusToolName,
		domain.DeployLogsToolName,
		domain.RollbackReleaseToolName,
	} {
		assert.Contains(t, policy.AllowTools, name)
	}
}

func TestOnlyQAHoldsTheRollback(t *testing.T) {
	others := map[string]domain.ToolPolicy{
		"developer":        developerToolPolicy(),
		"mobile-developer": mobileDeveloperToolPolicy(),
		"architect":        architectToolPolicy(),
		"product-manager":  productManagerToolPolicy(),
	}
	for role, policy := range others {
		t.Run(role, func(t *testing.T) {
			assert.NotContains(t, policy.AllowTools, domain.RollbackReleaseToolName)
		})
	}
}

func TestRollbackSurvivesOnlyInDone(t *testing.T) {
	assert.Contains(t, qaRunPolicyIn(t, domain.TaskColumnDone).AllowTools, domain.RollbackReleaseToolName)

	for _, column := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA,
		domain.TaskColumnPMUAT,
		domain.TaskColumnCodeReview,
	} {
		t.Run(string(column), func(t *testing.T) {
			assert.NotContains(t, qaRunPolicyIn(t, column).AllowTools, domain.RollbackReleaseToolName)
		})
	}
}

func TestDeployWatchReadToolsSurviveEveryColumn(t *testing.T) {
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA,
		domain.TaskColumnDone,
	} {
		t.Run(string(column), func(t *testing.T) {
			allow := qaRunPolicyIn(t, column).AllowTools
			assert.Contains(t, allow, domain.DeployStatusToolName)
			assert.Contains(t, allow, domain.DeployLogsToolName)
		})
	}
}

func TestWorkspaceUpliftNeverGrantsTheRollback(t *testing.T) {
	uplifted := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"run_terminal"}})
	assert.NotContains(t, uplifted.AllowTools, domain.RollbackReleaseToolName)
	assert.NotContains(t, uplifted.AllowTools, domain.MergePullRequestToolName)
}

func pmRunPolicyIn(t *testing.T, column domain.TaskColumn) domain.ToolPolicy {
	t.Helper()
	stage, typeDef := stageFor(t, "task", column)
	return domain.RestrictToolsForStage(
		domain.UpliftWorkspaceTools(
			domain.MergeToolPolicy(domain.ToolPolicy{}, productManagerToolPolicy()),
		),
		stage, typeDef,
	)
}

func TestPMUATRunPolicyLosesCodeExplorationTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT} {
		t.Run(string(column), func(t *testing.T) {
			policy := pmRunPolicyIn(t, column)
			for _, name := range domain.CodeExplorationTools {
				assert.NotContains(t, policy.AllowTools, name)
			}
			assert.Contains(t, policy.AllowTools, "browser_navigate")
			assert.Contains(t, policy.AllowTools, "review_criterion")
		})
	}
}

func TestPMKeepsCodeExplorationToolsOutsideUATColumns(t *testing.T) {
	policy := pmRunPolicyIn(t, domain.TaskColumnTodo)
	for _, name := range domain.CodeExplorationTools {
		assert.Contains(t, policy.AllowTools, name)
	}
}

func TestQARunPolicyKeepsItsCodeToolsInVerdictColumns(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnInQA, domain.TaskColumnReadyForQA} {
		t.Run(string(column), func(t *testing.T) {
			policy := qaRunPolicyIn(t, column)
			assert.Contains(t, policy.AllowTools, "read_file")
			assert.Contains(t, policy.AllowTools, "get_repo_tree")
			assert.Contains(t, policy.AllowTools, "grep_code")
			assert.Contains(t, policy.AllowTools, "get_task_pull_request")
		})
	}
}
