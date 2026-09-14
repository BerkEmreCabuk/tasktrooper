package code_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// overlayFixture is a git repo whose indexed state (the commit) and live
// state (uncommitted edits) diverge: NewHelper was added and gone.go deleted
// after the commit the fake index reflects.
type overlayFixture struct {
	root  string
	ctx   context.Context
	kit   *code.ToolKit
	store *fakeIndexStore
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	base := []string{"-C", root, "-c", "user.email=test@test", "-c", "user.name=test"}
	out, err := exec.Command("git", append(base, args...)...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func setupOverlayFixture(t *testing.T, stampCommit bool) overlayFixture {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))

	committed := "package pkg\n\nfunc OldThing() string { return \"old\" }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "main.go"), []byte(committed), 0o644))
	ghost := "package pkg\n\nfunc GhostFunc() string { return \"ghost\" }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "gone.go"), []byte(ghost), 0o644))

	runGit(t, root, "init", "-q")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-q", "-m", "indexed state")
	head := runGit(t, root, "rev-parse", "HEAD")

	// Agent edits after the indexed commit: add NewHelper, delete gone.go.
	edited := committed + "\nfunc NewHelper() string { return OldThing() + \" helper\" }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "main.go"), []byte(edited), 0o644))
	require.NoError(t, os.Remove(filepath.Join(root, "pkg", "gone.go")))

	sessionID := uuid.New()
	sid := sessionID
	commitSHA := ""
	if stampCommit {
		commitSHA = head
	}
	store := &fakeIndexStore{
		index: domain.WorkspaceIndex{
			ID:        uuid.New(),
			SessionID: &sid,
			RootPath:  root,
			Status:    domain.IndexStatusCompleted,
			CommitSHA: commitSHA,
		},
		chunks: []domain.WorkspaceChunk{
			{
				FilePath:   "pkg/gone.go",
				SymbolName: "GhostFunc",
				StartLine:  3,
				EndLine:    3,
				Signature:  "func GhostFunc() string",
				Content:    "func GhostFunc() string { return \"ghost\" }",
			},
			{
				FilePath:   "pkg/main.go",
				SymbolName: "OldThing",
				StartLine:  3,
				EndLine:    3,
				Signature:  "func OldThing() string",
				Content:    "func OldThing() string { return \"old\" }",
			},
		},
		symbols: []domain.WorkspaceSymbol{
			{FilePath: "pkg/gone.go", Kind: "function", Name: "GhostFunc", Signature: "func GhostFunc() string", StartLine: 3, EndLine: 3},
			{FilePath: "pkg/main.go", Kind: "function", Name: "OldThing", Signature: "func OldThing() string", StartLine: 3, EndLine: 3},
		},
	}

	mapperSvc := mapper.NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50})
	kit := code.NewToolKit(store, &fakeLLM{embedding: []float32{0.1, 0.2}}, mapperSvc, domain.IndexerConfig{TopK: 5}, domain.GraphConfig{MaxExpansionDepth: 2, MaxExpandedChunks: 8}, "embed-model", false)
	ctx := registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), sessionID),
		root,
	)
	return overlayFixture{root: root, ctx: ctx, kit: kit, store: store}
}

func execTool(t *testing.T, fx overlayFixture, toolIdx int, args string) domain.ToolResult {
	t.Helper()
	return code.NewExecutors(fx.kit)[toolIdx].Execute(fx.ctx, args)
}

func TestOverlayCodebaseSearchSeesUnpushedEditsAndDropsGhosts(t *testing.T) {
	for name, stamp := range map[string]bool{"diffVsCommitSHA": true, "gitStatusFallback": false} {
		t.Run(name, func(t *testing.T) {
			fx := setupOverlayFixture(t, stamp)

			result := execTool(t, fx, 0, `{"query":"NewHelper"}`)
			require.False(t, result.IsError, result.Content)

			var resp struct {
				Results []struct {
					FilePath   string `json:"file_path"`
					SymbolName string `json:"symbol_name"`
					Source     string `json:"source"`
				} `json:"results"`
			}
			require.NoError(t, json.Unmarshal([]byte(result.Content), &resp))

			foundLive := false
			for _, r := range resp.Results {
				require.NotEqual(t, "pkg/gone.go", r.FilePath, "deleted file must not ghost into results")
				if r.SymbolName == "NewHelper" {
					foundLive = true
					require.Equal(t, "workspace", r.Source)
					require.Equal(t, "pkg/main.go", r.FilePath)
				}
			}
			require.True(t, foundLive, "unpushed NewHelper must be searchable: %s", result.Content)
		})
	}
}

func TestOverlaySkeletonPrefersLiveSymbols(t *testing.T) {
	fx := setupOverlayFixture(t, true)

	result := execTool(t, fx, 3, `{"symbol_name":"NewHelper"}`)
	require.False(t, result.IsError, result.Content)
	require.Contains(t, result.Content, "NewHelper")
	require.Contains(t, result.Content, "pkg/main.go")

	ghost := execTool(t, fx, 3, `{"symbol_name":"GhostFunc"}`)
	require.True(t, ghost.IsError, "deleted symbol must not resolve from stale index: %s", ghost.Content)
}

func TestOverlayExpandSymbolContextWalksLiveGraph(t *testing.T) {
	fx := setupOverlayFixture(t, true)

	result := execTool(t, fx, 4, `{"symbol_name":"NewHelper"}`)
	require.False(t, result.IsError, result.Content)
	require.Contains(t, result.Content, "NewHelper")
	// NewHelper calls OldThing; the live call graph should surface it.
	require.Contains(t, result.Content, "OldThing")
	require.NotContains(t, result.Content, "GhostFunc")
}

func TestOverlayInactiveKeepsIndexBehavior(t *testing.T) {
	fx := setupOverlayFixture(t, true)
	// Clean tree: revert edits so the workspace matches the indexed commit.
	runGit(t, fx.root, "checkout", "--", ".")
	runGit(t, fx.root, "clean", "-fdq")

	result := execTool(t, fx, 0, `{"query":"GhostFunc"}`)
	require.False(t, result.IsError, result.Content)
	require.Contains(t, result.Content, "GhostFunc", "clean tree keeps plain index results")
	require.NotContains(t, result.Content, `"source":"workspace"`)
}
