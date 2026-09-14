package indexer

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Injector struct {
	store          port.IndexStore
	llm            port.LLMClient
	mapper         *mapper.Service
	embeddingModel string
	graphCfg       domain.GraphConfig
	rewriteEnabled bool
}

// SetQueryRewrite enables multi-query retrieval: the query text may be
// rewritten into up to two extra code-search queries before searching. Whether
// a given injection actually rewrites is the caller's call via
// InjectOptions.RewriteQuery — only raw human prose (chat) is worth the model
// turn. The rewrite runs on the active provider's default model.
func (i *Injector) SetQueryRewrite(enabled bool) {
	i.rewriteEnabled = enabled
}

func NewInjector(
	store port.IndexStore,
	llm port.LLMClient,
	mapperSvc *mapper.Service,
	embeddingModel string,
	graphCfg domain.GraphConfig,
) *Injector {
	return &Injector{
		store:          store,
		llm:            llm,
		mapper:         mapperSvc,
		embeddingModel: embeddingModel,
		graphCfg:       graphCfg,
	}
}

// buildQueries returns the original query plus up to two LLM-rewritten
// code-search variants (when query rewriting is enabled). Rewrite failures
// silently degrade to the original query only.
func (i *Injector) buildQueries(ctx context.Context, query string) []string {
	queries := []string{query}
	if !i.rewriteEnabled || i.llm == nil {
		return queries
	}
	resp, err := i.llm.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: "Rewrite the user's task into at most 2 short code-search queries (identifiers, function names, technical terms). One query per line. Output only the queries."},
			{Role: domain.RoleUser, Content: query},
		},
	})
	if err != nil {
		return queries
	}
	for _, line := range strings.Split(resp.Message.Content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "-*0123456789. "))
		if line == "" || strings.EqualFold(line, query) {
			continue
		}
		queries = append(queries, line)
		if len(queries) >= 3 {
			break
		}
	}
	return queries
}

// interleaveChunks round-robins the per-query result lists (each already
// ranked) into one deduplicated list of at most topK chunks.
func interleaveChunks(lists [][]domain.WorkspaceChunk, topK int, seen map[string]struct{}) []domain.WorkspaceChunk {
	var out []domain.WorkspaceChunk
	for pos := 0; len(out) < topK; pos++ {
		progressed := false
		for _, list := range lists {
			if pos >= len(list) {
				continue
			}
			progressed = true
			ch := list[pos]
			key := chunkKey(ch)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ch)
			if len(out) >= topK {
				return out
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

func (i *Injector) InjectContext(ctx context.Context, sessionID uuid.UUID, messages []domain.Message, opts domain.InjectOptions) ([]domain.Message, error) {
	var idx domain.WorkspaceIndex
	var err error
	projectID := registry.ProjectIDFromContext(ctx)
	if projectID != uuid.Nil {
		// Prefer the index of the branch this run is editing; fall back to the
		// default-branch index while the branch has none yet.
		if branch := registry.BranchFromContext(ctx); branch != "" {
			idx, err = i.store.GetIndexByProjectBranch(ctx, projectID, branch)
			if err != nil || idx.Status != domain.IndexStatusCompleted {
				idx, err = i.store.GetIndexByProject(ctx, projectID)
			}
		} else {
			idx, err = i.store.GetIndexByProject(ctx, projectID)
		}
	} else {
		idx, err = i.store.GetIndexBySession(ctx, sessionID)
	}
	if err != nil {
		return messages, nil
	}
	if idx.Status != domain.IndexStatusCompleted {
		return messages, nil
	}

	query := lastUserMessage(messages)
	if query == "" && opts.TargetSymbol == "" {
		return messages, nil
	}

	topK := opts.TopK
	if topK <= 0 {
		topK = 5
	}

	seen := make(map[string]struct{})
	var chunks []domain.WorkspaceChunk

	if opts.TargetSymbol != "" && opts.ExpandGraph && i.graphCfg.Enabled {
		expanded, err := i.expandGraphChunks(ctx, idx, opts)
		if err != nil {
			return messages, fmt.Errorf("expand graph context: %w", err)
		}
		for _, ch := range expanded {
			key := chunkKey(ch)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			chunks = append(chunks, ch)
		}
	}

	if query != "" {
		queries := []string{query}
		if opts.RewriteQuery {
			queries = i.buildQueries(ctx, query)
		}
		var lists [][]domain.WorkspaceChunk
		for _, q := range queries {
			queryEmb, err := i.llm.Embed(ctx, q, i.embeddingModel)
			if err != nil {
				if len(lists) > 0 {
					break
				}
				return messages, fmt.Errorf("embed query: %w", err)
			}
			found, err := i.store.SearchChunksHybrid(ctx, idx.ID, q, queryEmb, topK)
			if err != nil {
				if len(lists) > 0 {
					break
				}
				return messages, fmt.Errorf("search chunks: %w", err)
			}
			lists = append(lists, found)
		}
		for _, ch := range interleaveChunks(lists, topK, seen) {
			chunks = append(chunks, ch)
		}
	}

	if len(chunks) == 0 && !opts.IncludeTree && !opts.IncludeSkeleton {
		return messages, nil
	}

	if opts.MaxChunkTokens > 0 {
		chunks = trimChunksByTokenBudget(chunks, opts.MaxChunkTokens)
	}

	var fanIn map[string]int
	if opts.IncludeSkeleton {
		if edges, edgeErr := i.store.ListEdges(ctx, idx.ID); edgeErr == nil && len(edges) > 0 {
			fanIn = make(map[string]int, len(edges))
			for _, e := range edges {
				if e.ToFile != "" && e.ToFile != e.FromFile {
					fanIn[e.ToFile]++
				}
			}
		}
	}
	// The skeleton must describe the tree the agent is actually editing (the
	// task workspace), not the shared repo root the index was built from.
	rootPath := idx.RootPath
	if ws := registry.EffectiveWorkspaceDir(ctx); ws != "" {
		rootPath = ws
	}
	content := formatInjectMessage(idx, chunks, opts, i.mapper, rootPath, fanIn)
	if content == "" {
		return messages, nil
	}

	systemMsg := domain.Message{
		Role:    domain.RoleSystem,
		Content: content,
	}
	return append([]domain.Message{systemMsg}, messages...), nil
}

func (i *Injector) expandGraphChunks(ctx context.Context, idx domain.WorkspaceIndex, opts domain.InjectOptions) ([]domain.WorkspaceChunk, error) {
	symbols, err := i.store.SearchSymbols(ctx, opts.TargetSymbol, idx.ID)
	if err != nil {
		return nil, err
	}
	if len(symbols) == 0 {
		return nil, nil
	}

	target := symbols[0]
	if opts.TargetFilePath != "" {
		for _, sym := range symbols {
			if sym.FilePath == opts.TargetFilePath {
				target = sym
				break
			}
		}
	}

	maxChunks := i.graphCfg.MaxExpandedChunks
	if maxChunks <= 0 {
		maxChunks = 8
	}

	targetRef := graph.SymbolRef{
		FilePath:   target.FilePath,
		SymbolName: target.Name,
		Kind:       target.Kind,
	}
	edges, err := i.store.ListEdges(ctx, idx.ID)
	if err != nil {
		return nil, err
	}

	g := buildGraphFromEdges(edges)
	lookup := symbolLookup{symbols: symbols}
	depth := i.graphCfg.MaxExpansionDepth
	if depth <= 0 {
		depth = 2
	}
	refs, _ := graph.ExpandContext(g, lookup, targetRef, target.FilePath, depth, maxChunks)

	seen := make(map[string]struct{})
	var chunks []domain.WorkspaceChunk
	for _, ref := range refs {
		key := ref.Key()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ch, err := i.store.GetChunkBySymbol(ctx, idx.ID, ref.FilePath, ref.SymbolName)
		if err != nil {
			continue
		}
		chunks = append(chunks, ch)
	}

	if len(chunks) == 0 {
		ch, err := i.store.GetChunkBySymbol(ctx, idx.ID, target.FilePath, target.Name)
		if err == nil {
			chunks = append(chunks, ch)
		}
	}
	return chunks, nil
}

type symbolLookup struct {
	symbols []domain.WorkspaceSymbol
}

func (l symbolLookup) Lookup(filePath, symbolName string) (graph.SymbolRef, bool) {
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

func formatInjectMessage(idx domain.WorkspaceIndex, chunks []domain.WorkspaceChunk, opts domain.InjectOptions, mapperSvc *mapper.Service, rootPath string, fanIn map[string]int) string {
	var b strings.Builder

	if opts.IncludeTree && idx.TreeText != "" {
		b.WriteString("## Repository structure\n")
		b.WriteString(idx.TreeText)
		if !strings.HasSuffix(idx.TreeText, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	if opts.IncludeSkeleton && mapperSvc != nil && rootPath != "" {
		skeleton, err := mapperSvc.BuildSkeletonRanked(rootPath, fanIn)
		if err == nil && skeleton != "" {
			b.WriteString("## Code skeleton\n")
			b.WriteString(skeleton)
			if !strings.HasSuffix(skeleton, "\n") {
				b.WriteByte('\n')
			}
			b.WriteByte('\n')
		}
	}

	if len(chunks) > 0 {
		b.WriteString("## Relevant code\n")
		for _, ch := range chunks {
			header := fmt.Sprintf("### %s:%s (lines %d-%d)", ch.FilePath, ch.SymbolName, ch.StartLine, ch.EndLine)
			if ch.SymbolName == "" {
				header = fmt.Sprintf("### %s (lines %d-%d)", ch.FilePath, ch.StartLine, ch.EndLine)
			}
			b.WriteString(header)
			b.WriteByte('\n')
			lang := ch.Language
			if lang == "" {
				lang = "text"
			}
			b.WriteString("```")
			b.WriteString(lang)
			b.WriteByte('\n')
			if ch.Signature != "" && !strings.Contains(ch.Content, ch.Signature) {
				b.WriteString(ch.Signature)
				b.WriteByte('\n')
			}
			b.WriteString(ch.Content)
			if !strings.HasSuffix(ch.Content, "\n") {
				b.WriteByte('\n')
			}
			b.WriteString("```\n\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func lastUserMessage(messages []domain.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

func chunkKey(ch domain.WorkspaceChunk) string {
	return ch.FilePath + ":" + ch.SymbolName + ":" + fmt.Sprintf("%d-%d", ch.StartLine, ch.EndLine)
}

func trimChunksByTokenBudget(chunks []domain.WorkspaceChunk, maxTokens int) []domain.WorkspaceChunk {
	const charsPerToken = 4
	maxChars := maxTokens * charsPerToken
	used := 0
	var result []domain.WorkspaceChunk
	for _, ch := range chunks {
		size := len(ch.Content) + len(ch.Signature)
		if used+size > maxChars && len(result) > 0 {
			break
		}
		result = append(result, ch)
		used += size
	}
	return result
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
