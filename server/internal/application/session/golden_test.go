package session

import (
	gocontext "context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden regenerates every golden fixture in this package from the
// CURRENT implementation: `go test ./internal/application/session/... -run Golden -update`.
// This is the ONLY flag.Bool("update", ...) registration for this package's
// test binary — session_test files share it, so it must not be duplicated.
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

func TestGoldenPrependProjectPrompt(t *testing.T) {
	cases := map[string]string{
		"simple":  "A todo app for personal task tracking.",
		"turkish": "Kişisel görev takibi için bir uygulama.",
		"empty":   "",
	}
	for name, desc := range cases {
		t.Run(name, func(t *testing.T) {
			out := prependProjectPrompt(nil, desc)
			assertGolden(t, "prepend_project_prompt_"+name, out[0].Content)
		})
	}
}

func TestGoldenWorkspaceSystemMessage(t *testing.T) {
	cases := map[string]string{
		"basic":  "/tmp/ws",
		"nested": "/data/ws/abc/session-1",
		"empty":  "",
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, "workspace_system_message_"+name, workspaceSystemMessage(dir).Content)
		})
	}
}

func TestGoldenMentionContextMessage(t *testing.T) {
	id := uuid.MustParse("c072dfab-2c61-425f-b66b-38e51faa8989")
	assertGolden(t, "mention_context_agent_and_repo", mentionContextMessage([]mentionCandidate{
		{kind: "agent", name: "QA Agent", detail: "runs QA"},
		{kind: "repository", name: "local-llm"},
	}))
	assertGolden(t, "mention_context_with_id", mentionContextMessage([]mentionCandidate{
		{kind: "project", id: id, name: "acme", detail: "mobile"},
	}))
	assertGolden(t, "mention_context_no_agent", mentionContextMessage([]mentionCandidate{
		{kind: "project", name: "X"},
	}))
}

type goldenCapturingChatClient struct {
	seen  domain.AgentRequest
	reply string
}

func (c *goldenCapturingChatClient) Chat(_ gocontext.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	c.seen = req
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: c.reply}}, nil
}

func (c *goldenCapturingChatClient) ChatStream(ctx gocontext.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return c.Chat(ctx, req)
}

func (c *goldenCapturingChatClient) Models(gocontext.Context) ([]string, error) { return nil, nil }

func (c *goldenCapturingChatClient) Embed(gocontext.Context, string, string) ([]float32, error) {
	return nil, nil
}

func TestGoldenGenerateTitleSystemPrompt(t *testing.T) {
	client := &goldenCapturingChatClient{reply: "A title"}
	g := NewLLMTitleGenerator(client)
	_, err := g.GenerateTitle(gocontext.Background(), "hello", "hi there", "test-model", domain.LLMProviderAnthropic)
	require.NoError(t, err)
	assertGolden(t, "generate_title_system_prompt", client.seen.Messages[0].Content)
}
