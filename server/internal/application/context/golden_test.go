package context

import (
	gocontext "context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden regenerates every golden fixture in this package from the
// CURRENT implementation: `go test ./internal/application/context/... -run Golden -update`.
// This is the ONLY flag.Bool("update", ...) registration for this package's
// test binary — context_test files share it, so it must not be duplicated.
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

func TestGoldenSummarizeSystemPrompt(t *testing.T) {
	client := &capturingChatClient{reply: "condensed"}
	s := NewLLMSummarizer(client)
	_, err := s.SummarizeFor(gocontext.Background(), []domain.Message{{Role: domain.RoleUser, Content: "did work"}}, "m", "")
	require.NoError(t, err)
	assertGolden(t, "summarize_system_prompt", client.seen.Messages[0].Content)
}

func TestGoldenSummarizeRollingWrapper(t *testing.T) {
	cases := map[string]string{
		"short_summary": "Investigated the auth bug and fixed the token refresh race.",
		"turkish":       "Kimlik doğrulama hatasını araştırdım ve düzelttim.",
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			client := &capturingChatClient{reply: reply}
			s := NewLLMSummarizer(client)
			budget := Budget{SummarizeThreshold: 1, KeepRecentMessages: 1}
			messages := []domain.Message{
				{Role: domain.RoleSystem, Content: "you are an agent"},
				{Role: domain.RoleUser, Content: "a long first turn worth condensing"},
				{Role: domain.RoleAssistant, Content: "an answer"},
				{Role: domain.RoleUser, Content: "the newest turn"},
			}
			out, err := SummarizeRollingFor(gocontext.Background(), budget, s, messages, "m", "")
			require.NoError(t, err)
			require.NotEmpty(t, out)
			assertGolden(t, "summarize_rolling_wrapper_"+name, out[len(out)-2].Content)
		})
	}
}

func TestGoldenClearedToolResultNote(t *testing.T) {
	assertGolden(t, "cleared_tool_result_note_named", ClearedToolResultNote("read_file"))
	assertGolden(t, "cleared_tool_result_note_empty", ClearedToolResultNote(""))
	assertGolden(t, "cleared_tool_result_note_other", ClearedToolResultNote("run_terminal"))
}
