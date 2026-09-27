package runtime

import (
	"encoding/json"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestQueryRuntimeLogsTruncatedNoteProseUnchanged pins the exact "note" field
// query_runtime_logs hands back to the model when the provider capped the
// window, ahead of moving it into
// catalog/system/prompts/tool_results/runtime_logs_truncated.md.
func TestQueryRuntimeLogsTruncatedNoteProseUnchanged(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, "apps/web")
	fx.provider.logsPage = domain.RuntimeLogPage{Truncated: true}

	tool := &queryRuntimeLogsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "the provider capped this window; older entries in range were not returned"
	if got := out["note"]; got != want {
		t.Errorf("note = %v, want %q", got, want)
	}
}
