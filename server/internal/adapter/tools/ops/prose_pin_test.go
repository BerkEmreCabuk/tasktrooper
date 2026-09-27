package ops

import "testing"

// TestRepositoryIDHelpUnchanged pins the exact wording deployTargetTool,
// updateDeployTargetTool and recordLocalDeployTool hand back for an
// unresolvable repository_id, ahead of moving it into catalog/system/guards.
func TestRepositoryIDHelpUnchanged(t *testing.T) {
	want := "invalid repository_id: pass the repository_id UUID from your task snapshot, or omit it to use the current task's repository"
	if repositoryIDHelp != want {
		t.Errorf("repositoryIDHelp = %q, want %q", repositoryIDHelp, want)
	}
}
