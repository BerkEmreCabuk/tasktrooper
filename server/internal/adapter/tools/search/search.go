package search

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

const ToolName = "web_search"

const searchTimeout = 15 * time.Second

// maxSearchResponseBytes caps what a search engine can make this process
// allocate. 512 KB is far past any real result page — the largest page this was
// built against is ~120 KB — and it is the only thing standing between a
// hostile answer (or anything that can answer as one) and an OOM.
const maxSearchResponseBytes = 512 * 1024

type args struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

// searchTool is a keyless web search: it asks the same HTML endpoints a browser
// asks and reads the answer. There is no provider and no credential, so there is
// nothing to configure and nothing to leak — see engine.go for how the two
// backends are ordered and what happens when one of them puts up a bot wall.
type searchTool struct {
	maxResults int
	policy     urlguard.Policy
	limiter    *limiter
	cache      *cache
	now        func() time.Time
}

// Option customises the tool at construction.
type Option func(*searchTool)

// WithURLPolicy replaces the destination policy; tests use it to point the
// engine endpoints at an httptest server.
func WithURLPolicy(p urlguard.Policy) Option {
	return func(s *searchTool) { s.policy = p }
}

// WithPolitenessGap replaces the minimum spacing between outbound requests.
// Zero disables the wait, which is what tests want — the production value costs
// a real second per hop.
func WithPolitenessGap(d time.Duration) Option {
	return func(s *searchTool) { s.limiter.gap = d }
}

// WithClock replaces time.Now and the sleep used to honour the politeness gap.
// Tests only: it is how the gap is asserted without spending it.
func WithClock(now func() time.Time, sleep func(context.Context, time.Duration) error) Option {
	return func(s *searchTool) {
		s.now = now
		s.limiter.now = now
		s.limiter.sleep = sleep
	}
}

// New builds the tool. maxResults is the default cap; a call may ask for fewer
// or up to 10.
//
// The limiter and the cache are per-tool because the runtime builds exactly one
// of these per process (see registerBuiltinTools) — per-tool state is per-process
// state, and keeping it off package scope is what lets tests run in parallel
// without leaking each other's queries.
func New(maxResults int, opts ...Option) port.ToolExecutor {
	if maxResults <= 0 {
		maxResults = 5
	}
	t := &searchTool{
		maxResults: maxResults,
		policy:     urlguard.Default(),
		limiter:    newLimiter(minRequestGap),
		cache:      newCache(cacheEntries, cacheTTL),
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (s *searchTool) Name() string { return ToolName }

func (s *searchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        ToolName,
			Description: "Search the web and return a list of relevant results with titles, URLs, and snippets. Use this to find current information, news, documentation, or anything that requires up-to-date knowledge.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 5, max: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (s *searchTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a args
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return domain.ToolResult{Name: ToolName, Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}
	if a.Query == "" {
		return domain.ToolResult{Name: ToolName, Content: "query is required", IsError: true}
	}
	maxResults := s.maxResults
	if a.MaxResults > 0 {
		// Clamp rather than fall back. A model that asks for 20 wants more than
		// the default, and quietly handing it the default read as the argument
		// being ignored — the same call was then made again with the same
		// number. 0 and below stay the "unset" spelling.
		maxResults = min(a.MaxResults, 10)
	}

	content, source, err := s.search(ctx, a.Query, maxResults)
	if err != nil {
		// The message already says what to do instead; wrapping it in
		// "search error:" only made an earlier version read like bad luck the
		// model should retry.
		return domain.ToolResult{Name: ToolName, Content: err.Error(), IsError: true}
	}
	log.Debug().Str("source", source).Str("query", a.Query).Msg("web search")
	return domain.ToolResult{Name: ToolName, Content: content}
}
