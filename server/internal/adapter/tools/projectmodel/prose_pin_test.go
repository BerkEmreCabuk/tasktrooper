package projectmodel

import (
	"context"
	"testing"
)

// TestResolveRepositoryIDMessageUnchanged pins the exact wording, ahead of
// moving it into catalog/system/guards — it is byte-identical to
// adapter/tools/runtime's own resolveRepositoryID, and both now share
// guard.tool_repository_required.
func TestResolveRepositoryIDMessageUnchanged(t *testing.T) {
	if _, err := resolveRepositoryID(context.Background(), "", "get_project_brief"); err == nil ||
		err.Error() != "get_project_brief needs a repository in context or repository_id; this run has none" {
		t.Errorf("resolveRepositoryID error = %v", err)
	}
}
