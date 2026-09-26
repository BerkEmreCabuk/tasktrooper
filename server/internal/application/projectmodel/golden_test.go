package projectmodel

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden regenerates every golden fixture in this package from the
// CURRENT implementation: `go test ./internal/application/projectmodel/... -run Golden -update`.
var updateGolden = flag.Bool("update", false, "update golden fixtures")

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file %s (run with -update)", path)
	assert.Equal(t, string(want), got)
}

func TestGoldenToolsNote(t *testing.T) {
	assertGolden(t, "tools_note_empty_policy_allows_all", ToolsNote(domain.ToolPolicy{}))
	assertGolden(t, "tools_note_subset", ToolsNote(domain.ToolPolicy{AllowTools: []string{"list_runtime_errors", "get_environment", "run_terminal"}}))
	assertGolden(t, "tools_note_none_allowed", ToolsNote(domain.ToolPolicy{AllowTools: []string{"run_terminal", "write_file"}}))
}
