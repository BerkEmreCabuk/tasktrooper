package indexer

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden regenerates every golden fixture in this package from the
// CURRENT implementation: `go test ./internal/application/indexer/... -run Golden -update`.
// This is the ONLY flag.Bool("update", ...) registration for this package's
// test binary — indexer_test files share it, so it must not be duplicated.
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

type goldenCapturingLLM struct {
	seen  domain.AgentRequest
	reply string
}

func (f *goldenCapturingLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	f.seen = req
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: f.reply}}, nil
}

func (f *goldenCapturingLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return f.Chat(ctx, req)
}

func (f *goldenCapturingLLM) Models(context.Context) ([]string, error) { return nil, nil }

func (f *goldenCapturingLLM) Embed(context.Context, string, string) ([]float32, error) {
	return nil, nil
}

func TestGoldenBuildQueriesSystemPrompt(t *testing.T) {
	llm := &goldenCapturingLLM{reply: "one\ntwo"}
	i := &Injector{llm: llm, rewriteEnabled: true}
	i.buildQueries(context.Background(), "how does auth work")
	assertGolden(t, "build_queries_system_prompt", llm.seen.Messages[0].Content)
}

func TestGoldenFormatInjectMessage(t *testing.T) {
	idx := domain.WorkspaceIndex{TreeText: "project/\n├── pkg/\n"}
	chunk := domain.WorkspaceChunk{
		FilePath: "internal/auth/encrypt.go", SymbolName: "Encrypt",
		StartLine: 42, EndLine: 78, Language: "go",
		Signature: "func Encrypt(password string) (string, error)",
		Content:   "func Encrypt(password string) (string, error) {\n\treturn hash(password)\n}",
	}
	unnamedChunk := domain.WorkspaceChunk{
		FilePath: "README.md", StartLine: 1, EndLine: 3,
		Content: "# Project\nDoes things.",
	}

	assertGolden(t, "format_inject_tree_only", formatInjectMessage(idx, nil, domain.InjectOptions{IncludeTree: true}, nil, "", nil))
	assertGolden(t, "format_inject_chunks_only", formatInjectMessage(domain.WorkspaceIndex{}, []domain.WorkspaceChunk{chunk}, domain.InjectOptions{}, nil, "", nil))
	assertGolden(t, "format_inject_tree_and_chunks", formatInjectMessage(idx, []domain.WorkspaceChunk{chunk, unnamedChunk}, domain.InjectOptions{IncludeTree: true}, nil, "", nil))
}
