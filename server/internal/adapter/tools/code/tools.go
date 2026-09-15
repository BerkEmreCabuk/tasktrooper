package code

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ToolKit struct {
	IndexStore     port.IndexStore
	LLM            port.LLMClient
	Mapper         *mapper.Service
	Chunkers       *chunker.Registry
	EmbeddingModel string
	TopK           int
	GraphCfg       domain.GraphConfig
}

func NewToolKit(
	store port.IndexStore,
	llm port.LLMClient,
	mapperSvc *mapper.Service,
	indexerCfg domain.IndexerConfig,
	graphCfg domain.GraphConfig,
	embeddingModel string,
) *ToolKit {
	topK := indexerCfg.TopK
	if topK <= 0 {
		topK = 5
	}
	return &ToolKit{
		IndexStore:     store,
		LLM:            llm,
		Mapper:         mapperSvc,
		Chunkers:       chunker.DefaultRegistry(),
		EmbeddingModel: embeddingModel,
		TopK:           topK,
		GraphCfg:       graphCfg,
	}
}

func NewExecutors(kit *ToolKit) []port.ToolExecutor {
	if kit == nil {
		return nil
	}
	return []port.ToolExecutor{
		newCodebaseSearchTool(kit),
		newGrepCodeTool(kit),
		newGetRepoTreeTool(kit),
		newGetSymbolSkeletonTool(kit),
		newExpandSymbolContextTool(kit),
	}
}

func resolveProjectRoot(ctx context.Context) (string, error) {
	root := registry.EffectiveWorkspaceDir(ctx)
	if root == "" {
		return "", fmt.Errorf("workspace directory not set in context")
	}
	return root, nil
}

func resolveSessionID(ctx context.Context) (uuid.UUID, error) {
	id := registry.SessionIDFromContext(ctx)
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("session id not set in context")
	}
	return id, nil
}

func resolveIndex(ctx context.Context, store port.IndexStore) (domain.WorkspaceIndex, error) {
	if store == nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("index store not configured")
	}
	projectID := registry.ProjectIDFromContext(ctx)
	if projectID != uuid.Nil {
		// Board runs carry the task branch; prefer that branch's index so
		// search reflects the tree the agent is editing.
		if branch := registry.BranchFromContext(ctx); branch != "" {
			if idx, err := store.GetIndexByProjectBranch(ctx, projectID, branch); err == nil && idx.Status == domain.IndexStatusCompleted {
				return idx, nil
			}
		}
		idx, err := store.GetIndexByProject(ctx, projectID)
		if err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("get project index: %w", err)
		}
		return idx, nil
	}
	sessionID, err := resolveSessionID(ctx)
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}
	idx, err := store.GetIndexBySession(ctx, sessionID)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("get index: %w", err)
	}
	return idx, nil
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

// indexUnavailableError explains a missing semantic index in terms of what to
// do next. Without the second sentence the agent read "get project index: not
// found" as a transient glitch and called the same tool again every iteration —
// a code_review run burned seven iterations that way, all of them on
// codebase_search against an unindexed repository.
func indexUnavailableError(name string, cause error) domain.ToolResult {
	return toolError(name, fmt.Sprintf(
		"this repository has no semantic index yet (%v). %s cannot work without one — do not retry it. "+
			"Use grep_code for exact symbols and strings, get_repo_tree for structure, and read_file to read a file.",
		cause, name))
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: name, Content: string(raw), IsError: false}
}

func buildGraphFromEdges(edges []domain.WorkspaceEdge) *graph.DependencyGraph {
	g := graph.NewDependencyGraph()
	for _, e := range edges {
		kind := graph.EdgeCall
		if e.EdgeKind == string(graph.EdgeImport) {
			kind = graph.EdgeImport
		}
		g.AddEdge(graph.Edge{
			From: graph.SymbolRef{FilePath: e.FromFile, SymbolName: e.FromSymbol},
			To:   graph.SymbolRef{FilePath: e.ToFile, SymbolName: e.ToSymbol},
			Kind: kind,
		})
	}
	return g
}

type indexSymbolLookup struct {
	symbols []domain.WorkspaceSymbol
}

func (l indexSymbolLookup) Lookup(filePath, symbolName string) (graph.SymbolRef, bool) {
	for _, sym := range l.symbols {
		if sym.FilePath == filePath && sym.Name == symbolName {
			return graph.SymbolRef{
				FilePath:   sym.FilePath,
				SymbolName: sym.Name,
				Kind:       sym.Kind,
			}, true
		}
	}
	for _, sym := range l.symbols {
		if sym.Name == symbolName {
			return graph.SymbolRef{
				FilePath:   sym.FilePath,
				SymbolName: sym.Name,
				Kind:       sym.Kind,
			}, true
		}
	}
	return graph.SymbolRef{}, false
}

// The two tool names platform/runtime has to be able to say out loud.
//
// Exported here rather than duplicated there because a deployment with no
// working copy on its own disk registers exactly these two and none of the
// other three (see indexOnlyCodeTool), and a second copy of the strings is how
// a rename would silently start registering a filesystem tool on a host with no
// filesystem to serve it from.
const (
	CodebaseSearchToolName      = codebaseSearchToolName
	ExpandSymbolContextToolName = expandSymbolContextToolName
)
