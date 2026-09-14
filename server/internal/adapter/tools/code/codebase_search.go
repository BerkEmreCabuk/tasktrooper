package code

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const codebaseSearchToolName = "codebase_search"

type codebaseSearchArgs struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type codebaseSearchResult struct {
	FilePath   string `json:"file_path"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	SymbolName string `json:"symbol_name"`
	Signature  string `json:"signature"`
	Snippet    string `json:"snippet"`
	// Source marks where the match came from when unindexed edits exist:
	// "workspace" = live parse of a just-edited file, "index" = stored chunk.
	Source string `json:"source,omitempty"`
}

type codebaseSearchResponse struct {
	Results []codebaseSearchResult `json:"results"`
}

type codebaseSearchTool struct {
	kit *ToolKit
}

func newCodebaseSearchTool(kit *ToolKit) port.ToolExecutor {
	return &codebaseSearchTool{kit: kit}
}

func (t *codebaseSearchTool) Name() string {
	return codebaseSearchToolName
}

func (t *codebaseSearchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        codebaseSearchToolName,
			Description: "Semantic search over the indexed workspace codebase. Returns matching code chunks ranked by relevance.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Natural language or keyword query describing the code to find",
					},
					"top_k": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (t *codebaseSearchTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args codebaseSearchArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(codebaseSearchToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.Query == "" {
		return toolError(codebaseSearchToolName, "query is required")
	}
	if t.kit.IndexStore == nil {
		return toolError(codebaseSearchToolName, "index store not configured")
	}
	if t.kit.LLM == nil {
		return toolError(codebaseSearchToolName, "embedding client not configured")
	}

	// Resolve like every other code tool: repository (and branch) first, then
	// session. Board runs create a fresh session that owns no index, so the
	// old session-only lookup left this tool dead on the board path.
	idx, err := resolveIndex(ctx, t.kit.IndexStore)
	if err != nil {
		return indexUnavailableError(codebaseSearchToolName, err)
	}

	topK := args.TopK
	if topK <= 0 {
		topK = t.kit.TopK
	}

	embedding, err := t.kit.LLM.Embed(ctx, args.Query, t.kit.EmbeddingModel)
	if err != nil {
		if res, ok := embeddingUnavailableError(codebaseSearchToolName, err); ok {
			return res
		}
		return toolError(codebaseSearchToolName, fmt.Sprintf("embed query: %v", err))
	}

	chunks, err := t.kit.IndexStore.SearchChunksHybrid(ctx, idx.ID, args.Query, embedding, topK)
	if err != nil {
		return toolError(codebaseSearchToolName, fmt.Sprintf("search chunks: %v", err))
	}

	// Unindexed edits: drop stale index rows for touched files and search
	// those files live instead, so results reflect the tree as it is now.
	overlay := buildOverlay(ctx, t.kit, idx)
	source := ""
	if overlay != nil {
		source = "index"
		kept := chunks[:0]
		for _, ch := range chunks {
			if !overlay.isStale(ch.FilePath) {
				kept = append(kept, ch)
			}
		}
		chunks = kept
	}

	results := make([]codebaseSearchResult, 0, topK)
	if overlay != nil {
		liveCap := (topK + 1) / 2
		if len(chunks) < topK-liveCap {
			liveCap = topK - len(chunks)
		}
		for _, ch := range overlay.searchLive(args.Query, liveCap) {
			results = append(results, codebaseSearchResult{
				FilePath:   ch.FilePath,
				StartLine:  ch.StartLine,
				EndLine:    ch.EndLine,
				SymbolName: ch.SymbolName,
				Signature:  ch.Signature,
				Snippet:    ch.Content,
				Source:     "workspace",
			})
		}
	}
	for _, ch := range chunks {
		if len(results) >= topK {
			break
		}
		snippet := ch.Content
		if ch.Signature != "" && snippet == "" {
			snippet = ch.Signature
		}
		results = append(results, codebaseSearchResult{
			FilePath:   ch.FilePath,
			StartLine:  ch.StartLine,
			EndLine:    ch.EndLine,
			SymbolName: ch.SymbolName,
			Signature:  ch.Signature,
			Snippet:    snippet,
			Source:     source,
		})
	}

	return toolJSON(codebaseSearchToolName, codebaseSearchResponse{Results: results})
}

// embeddingFallbackTools names what still works when the embedding provider
// does not. Every branch below ends with it, because "semantic search failed"
// on its own leaves the agent with nowhere to go.
const embeddingFallbackTools = "use grep_code for exact symbols/strings, get_repo_tree to browse and read_file to read"

// embeddingUnavailableError turns an embedding provider's failure into an
// instruction, and reports false for any other error so the caller keeps its
// own wording.
//
// The failure used to reach the model as the provider's own sentence —
// `embed query: embeddings returned 402: {"detail":"Check your subscription on
// https://admin.mistral.ai/subscription"}` — which says nothing about what the
// agent should do instead, and reads like something that might work next time.
// One production run called codebase_search eight times against a provider
// whose plan was spent.
//
// The wording splits on whether the run can recover, because the two mistakes
// cost about the same:
//
//   - 401/402/403 cannot change inside a run, so the answer redirects the work
//     for good.
//   - A transport failure (DNS blip, TLS reset, client timeout) and a 429 both
//     clear on their own. Telling the agent to abandon semantic search over one
//     of those loses it for a whole run on a hiccup, so they get a bounded
//     retry instead of a verdict.
func embeddingUnavailableError(name string, err error) (domain.ToolResult, bool) {
	var unavailable *llm.EmbeddingUnavailableError
	var limited *llm.RateLimitError
	switch {
	case errors.As(err, &unavailable) && unavailable.StatusCode == 0:
		// The provider was never reached at all, and the next call may reach it.
		return toolError(name, fmt.Sprintf(
			"semantic search could not run just now: embedding provider could not be reached (%v). "+
				"Retry %s once after a moment, otherwise %s.",
			unavailable.Cause, name, embeddingFallbackTools)), true
	case errors.As(err, &unavailable):
		return toolError(name, fmt.Sprintf(
			"semantic search is unavailable on this server (embedding provider rejected the request: HTTP %d). "+
				"Use grep_code for exact symbols/strings, get_repo_tree to browse and read_file to read — "+
				"do not call %s again in this run.",
			unavailable.StatusCode, name)), true
	case errors.As(err, &limited):
		// The account limiter has already waited out one window by the time
		// this escapes, so the budget left here is one call, not a loop.
		return toolError(name, fmt.Sprintf(
			"semantic search could not run just now: embedding provider is rate-limited (HTTP %d%s). "+
				"Wait, then at most one retry — otherwise %s.",
			limited.StatusCode, retryAfterClause(limited.RetryAfter), embeddingFallbackTools)), true
	}
	return domain.ToolResult{}, false
}

// retryAfterClause states the wait the provider itself asked for, and says
// nothing when it asked for none: an invented number would read to the agent
// exactly like a measured one.
func retryAfterClause(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return fmt.Sprintf(", retry after %ds", int(d.Round(time.Second)/time.Second))
}
