package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const anthropicVersion = "2023-06-01"
const anthropicDefaultMaxTokens = 8192

// The claude-3 generation caps max_tokens at 4096, so sending the default made
// the API reject every request to those models with a 400.
const anthropicLegacyMaxTokens = 4096

func anthropicMaxTokens(model string) int {
	if strings.HasPrefix(model, "claude-3-opus") || strings.HasPrefix(model, "claude-3-haiku") {
		return anthropicLegacyMaxTokens
	}
	return anthropicDefaultMaxTokens
}

// resolveAnthropicMaxTokens is override (domain.AgentRequest.MaxTokens) when
// the caller set one — the agent loop enforcing its context budget's output
// reserve, or a utility call (the summarizer, a commit message rewrite)
// capping its own short answer — and the adapter's own per-model default
// otherwise, exactly the behaviour every request had before MaxTokens
// existed. A pipeline stage that never sets it (planner, intake, verifier,
// replanner, evolution) is unaffected: their schema output can legitimately
// be long.
func resolveAnthropicMaxTokens(model string, override int) int {
	if override > 0 {
		return override
	}
	return anthropicMaxTokens(model)
}

// knownAnthropicModels is returned as fallback when the API models endpoint is unavailable.
var knownAnthropicModels = []string{
	"claude-opus-5",
	"claude-sonnet-5",
	"claude-haiku-4-5",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-opus-4-6",
	"claude-sonnet-4-6",
}

type anthropicClient struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

func NewAnthropicClient(baseURL, model, apiKey string, timeout time.Duration) port.LLMClient {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &anthropicClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *anthropicClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

// --- request/response types ---

type anthropicRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	// System is a plain string when nothing caches it and the block form
	// ([]anthropicSystemBlock) when a breakpoint lands on it — only the block
	// form can carry cache_control. Both are valid wire shapes; keeping the
	// string for the uncached case leaves every existing request byte-identical.
	System       interface{}            `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	Tools        []anthropicTool        `json:"tools,omitempty"`
	Stream       bool                   `json:"stream,omitempty"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
	// ContextManagement asks the API to prune the transcript server-side. Nil
	// omits the field, which is every request that did not opt in — see
	// domain.AgentRequest.ClearToolResults for why that is the default.
	ContextManagement *anthropicContextManagement `json:"context_management,omitempty"`
}

// anthropicContextManagement clears stale content from the transcript before
// the model sees it. It PRUNES rather than summarizes — summarizing is
// compaction, a separate feature behind a different beta and a different edit
// type, and the two are easy to confuse by name.
type anthropicContextManagement struct {
	Edits []anthropicContextEdit `json:"edits"`
}

type anthropicContextEdit struct {
	Type string `json:"type"`
}

const (
	// clearToolUsesEdit drops old tool RESULTS and keeps the tool_use blocks
	// that name what was called. The split is the point: the run keeps its
	// ledger — which files it read, which commands it ran — and sheds only the
	// payloads, so it does not rediscover what it already found.
	clearToolUsesEdit = "clear_tool_uses_20250919"

	// contextManagementBeta gates the field above. Sending the field without
	// the header is rejected, so the two always travel together.
	contextManagementBeta = "context-management-2025-06-27"
)

// anthropicCacheControl marks a prompt-cache breakpoint. "ephemeral" with no
// ttl field is the 5-minute default; the 1h TTL costs twice as much to write
// and only pays off across gaps longer than five minutes, which an agent run
// does not have.
type anthropicCacheControl struct {
	Type string `json:"type"`
}

func ephemeralCache() *anthropicCacheControl {
	return &anthropicCacheControl{Type: "ephemeral"}
}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicOutputConfig struct {
	Format *anthropicOutputFormat `json:"format,omitempty"`
	// Effort is how hard the model thinks before answering: low, medium, high,
	// xhigh, max. Empty omits it and the model's own default stands.
	//
	// It shares output_config with Format because the API puts them there, not
	// because they are related: a request can carry either, both, or neither.
	Effort string `json:"effort,omitempty"`
}

type anthropicOutputFormat struct {
	Type   string                 `json:"type"`
	Schema map[string]interface{} `json:"schema"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Input        map[string]interface{} `json:"input,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
	Content      interface{}            `json:"content,omitempty"`
	Source       *anthropicImageSource  `json:"source,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

// anthropicUsage reports input_tokens EXCLUDING anything the cache handled:
// a fully cached turn comes back with input_tokens near zero and the real
// prompt size sitting in cache_read_input_tokens. domain.Usage promises the
// opposite (PromptTokens is the total), so toDomain adds them back.
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func (u anthropicUsage) toDomain() domain.Usage {
	prompt := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	return domain.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

// --- message translation ---

// anthropicImageBlocks renders images as the base64 source blocks Anthropic
// accepts. The shape is identical wherever an image can appear — inside a
// tool_result and inside a user turn — so both paths share it.
func anthropicImageBlocks(images []domain.ToolResultImage) []anthropicContent {
	blocks := make([]anthropicContent, 0, len(images))
	for _, img := range images {
		blocks = append(blocks, anthropicContent{
			Type: "image",
			Source: &anthropicImageSource{
				Type:      "base64",
				MediaType: img.MediaType,
				Data:      img.Data,
			},
		})
	}
	return blocks
}

// maxAnthropicBreakpoints is an API limit, not a tuning knob: a request with a
// fifth cache_control is rejected outright.
const maxAnthropicBreakpoints = 4

// markCacheable puts a breakpoint on a message's last content block and
// reports whether it landed. A block with nothing in it can't anchor a cache
// entry, so an empty message (or one ending in an empty text block) is skipped
// and its breakpoint stays in the budget for the next candidate.
func markCacheable(msg *anthropicMessage) bool {
	if msg == nil || len(msg.Content) == 0 {
		return false
	}
	last := &msg.Content[len(msg.Content)-1]
	if last.Type == "text" && last.Text == "" {
		return false
	}
	last.CacheControl = ephemeralCache()
	return true
}

func buildAnthropicRequest(model string, msgs []domain.Message, tools []domain.ToolDefinition, stream bool, respFormat *domain.ResponseFormat, cacheAnchor int) anthropicRequest {
	// Callers stack several system messages (agent persona + skills + rules, KPI
	// and memory blocks, workspace note, language rule, project context). Anthropic
	// takes a single system string, so they must be joined — assigning would keep
	// only the last one and silently drop the agent's whole instruction set.
	//
	// Only the ones that arrive BEFORE the first real turn are that persona head.
	// A system message the loop injects mid-run (budget warning, empty-turn nudge)
	// is an event at a point in the conversation, and hoisting it to the front
	// both misrepresents it and rewrites the cached prefix — see systemReminder.
	var systemParts []string
	var anthMsgs []anthropicMessage
	var pendingToolResults []anthropicContent
	seenTurn := false

	// Same invariant the OpenAI-compatible path enforces: every tool_use must be
	// answered by a tool_result. Anthropic rejects an unanswered one with a 400,
	// and the agent loop now trims mid-run to stay inside the context budget, so
	// an orphan is something this path has to survive rather than something the
	// caller can promise never to produce.
	msgs = normalizeToolPairing(msgs)

	flush := func() {
		if len(pendingToolResults) > 0 {
			anthMsgs = append(anthMsgs, anthropicMessage{Role: "user", Content: pendingToolResults})
			pendingToolResults = nil
		}
	}

	// The caller's anchor indexes domain messages; breakpoints go on Anthropic
	// ones, and the two don't line up — the leading system turns leave the array
	// for the system field and consecutive tool results collapse into one
	// message. So translate as we build: anchorMark ends up as the index of the
	// last Anthropic message covering msgs[:cacheAnchor].
	if cacheAnchor < 0 {
		cacheAnchor = 0
	}
	if cacheAnchor > len(msgs) {
		cacheAnchor = len(msgs)
	}
	anchorMark := -1
	markAnchor := func() {
		// Tool results still pending will become one more message, so they
		// belong to the head and the anchor sits on them.
		anchorMark = len(anthMsgs) - 1
		if len(pendingToolResults) > 0 {
			anchorMark++
		}
	}

	for i, m := range msgs {
		if i == cacheAnchor {
			markAnchor()
		}
		switch m.Role {
		case domain.RoleSystem:
			if strings.TrimSpace(m.Content) == "" {
				break
			}
			if !seenTurn {
				systemParts = append(systemParts, m.Content)
				break
			}
			// Mid-run injection: keep it where it happened, as a user turn the
			// wrapper marks as machine-generated. Consecutive user messages are
			// already routine here (a tool-result batch is followed by the next
			// user turn), so this needs no merging.
			flush()
			anthMsgs = append(anthMsgs, anthropicMessage{
				Role:    "user",
				Content: []anthropicContent{{Type: "text", Text: systemReminder(m.Content)}},
			})
		case domain.RoleUser:
			seenTurn = true
			flush()
			// A plain text block is the shape every user turn has always had;
			// only a turn carrying image attachments grows the extra blocks, so
			// the common case stays byte-for-byte identical on the wire.
			if len(m.Images) == 0 {
				anthMsgs = append(anthMsgs, anthropicMessage{
					Role:    "user",
					Content: []anthropicContent{{Type: "text", Text: m.Content}},
				})
				continue
			}
			var content []anthropicContent
			if m.Content != "" {
				content = append(content, anthropicContent{Type: "text", Text: m.Content})
			}
			content = append(content, anthropicImageBlocks(m.Images)...)
			anthMsgs = append(anthMsgs, anthropicMessage{Role: "user", Content: content})
		case domain.RoleAssistant:
			seenTurn = true
			flush()
			var content []anthropicContent
			if m.Content != "" {
				content = append(content, anthropicContent{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				inputMap := map[string]interface{}{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputMap)
				content = append(content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: inputMap,
				})
			}
			if len(content) > 0 {
				anthMsgs = append(anthMsgs, anthropicMessage{Role: "assistant", Content: content})
			}
		case domain.RoleTool:
			seenTurn = true
			// A plain string keeps the wire format the API has always accepted;
			// only a result that carries images needs the content-block form,
			// where each image rides next to the text inside the tool_result.
			var resultContent interface{} = m.Content
			if len(m.Images) > 0 {
				var blocks []anthropicContent
				if m.Content != "" {
					blocks = append(blocks, anthropicContent{Type: "text", Text: m.Content})
				}
				resultContent = append(blocks, anthropicImageBlocks(m.Images)...)
			}
			pendingToolResults = append(pendingToolResults, anthropicContent{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   resultContent,
			})
		}
	}
	if cacheAnchor == len(msgs) {
		markAnchor()
	}
	flush()

	var anthTools []anthropicTool
	for _, t := range tools {
		params := t.Function.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		anthTools = append(anthTools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: params,
		})
	}

	// Anthropic's structured outputs take a schema via output_config; there is
	// no schema-less JSON mode, so a bare json_object request becomes a system
	// instruction instead of a request parameter (which would 400).
	var outputConfig *anthropicOutputConfig
	if respFormat != nil {
		if respFormat.Schema != nil {
			outputConfig = &anthropicOutputConfig{
				Format: &anthropicOutputFormat{Type: "json_schema", Schema: respFormat.Schema},
			}
		} else {
			systemParts = append(systemParts, "Respond with a single valid JSON object only. No prose, no markdown code fences.")
		}
	}

	// Breakpoints, in the order the request renders: tools, then system, then
	// messages. Each one caches everything before it, so they are spent from
	// the most stable boundary outward and the budget can only be exhausted by
	// the last, least valuable candidate.
	budget := maxAnthropicBreakpoints

	// One breakpoint on the last tool definition caches the whole tool array —
	// tools render first, so nothing before it can move.
	if budget > 0 && len(anthTools) > 0 {
		anthTools[len(anthTools)-1].CacheControl = ephemeralCache()
		budget--
	}

	system := strings.Join(systemParts, "\n\n")
	var systemField interface{}
	switch {
	case system == "":
		// Leave it nil so `omitempty` drops the field, as before.
	case budget > 0:
		systemField = []anthropicSystemBlock{{
			Type:         "text",
			Text:         system,
			CacheControl: ephemeralCache(),
		}}
		budget--
	default:
		systemField = system
	}

	// The stable head: everything the run promised not to rewrite. This is the
	// breakpoint that survives a summarize, because StableTrim only ever cuts
	// after it.
	rolling := len(anthMsgs) - 1
	if budget > 0 && anchorMark >= 0 && anchorMark < rolling && markCacheable(&anthMsgs[anchorMark]) {
		budget--
	}

	// The rolling breakpoint: standard incremental multi-turn caching, where
	// each turn reads back everything the previous one wrote.
	if budget > 0 && rolling >= 0 {
		if markCacheable(&anthMsgs[rolling]) {
			budget--
		}
	}

	return anthropicRequest{
		Model:        model,
		MaxTokens:    anthropicMaxTokens(model),
		System:       systemField,
		Messages:     anthMsgs,
		Tools:        anthTools,
		Stream:       stream,
		OutputConfig: outputConfig,
	}
}

// applyRequestTuning folds the per-run knobs onto a built request.
//
// They are applied AFTER buildAnthropicRequest rather than passed into it on
// purpose. That function already takes six positional parameters and is called
// from a dozen cache tests; two more bools and strings at the end would be both
// unreadable at the call site and a rewrite of every one of those tests, for
// two fields that touch nothing the builder computes.
//
// Returns whether the request now needs a beta header, so the caller sets one
// only when there is something to gate.
func applyRequestTuning(payload *anthropicRequest, req domain.AgentRequest) (needsContextManagementBeta bool) {
	if req.Effort != "" {
		// output_config may not exist yet: effort and the structured-output
		// schema share the object but neither implies the other.
		if payload.OutputConfig == nil {
			payload.OutputConfig = &anthropicOutputConfig{}
		}
		payload.OutputConfig.Effort = req.Effort
	}
	if req.ClearToolResults {
		payload.ContextManagement = &anthropicContextManagement{
			Edits: []anthropicContextEdit{{Type: clearToolUsesEdit}},
		}
		return true
	}
	return false
}

func parseAnthropicContent(content []anthropicContent) (string, []domain.ToolCall) {
	var text strings.Builder
	var toolCalls []domain.ToolCall
	for _, block := range content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, domain.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: domain.FunctionCall{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	return text.String(), toolCalls
}

// --- LLMClient implementation ---

func (c *anthropicClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	payload := buildAnthropicRequest(model, req.Messages, req.Tools, false, req.ResponseFormat, req.CacheAnchorIndex)
	payload.MaxTokens = resolveAnthropicMaxTokens(model, req.MaxTokens)
	needsBeta := applyRequestTuning(&payload, req)
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(httpReq)
	if needsBeta {
		httpReq.Header.Set("anthropic-beta", contextManagementBeta)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return domain.AgentResponse{}, httpError(resp, respBody)
	}

	var anthResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthResp); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	text, toolCalls := parseAnthropicContent(anthResp.Content)
	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   text,
			ToolCalls: toolCalls,
		},
		Usage: anthResp.Usage.toDomain(),
	}, nil
}

type anthropicSSEEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	Usage anthropicUsage `json:"usage"`
}

type streamToolBlock struct {
	id   string
	name string
	args strings.Builder
}

func (c *anthropicClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	payload := buildAnthropicRequest(model, req.Messages, req.Tools, true, req.ResponseFormat, req.CacheAnchorIndex)
	payload.MaxTokens = resolveAnthropicMaxTokens(model, req.MaxTokens)
	needsBeta := applyRequestTuning(&payload, req)
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(httpReq)
	if needsBeta {
		httpReq.Header.Set("anthropic-beta", contextManagementBeta)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return domain.AgentResponse{}, httpError(resp, errBody)
	}

	var fullText strings.Builder
	toolBlocks := make(map[int]*streamToolBlock)
	var streamUsage anthropicUsage

	scanner := bufio.NewScanner(resp.Body)
	// SSE frames carrying tool-call arguments routinely exceed bufio's 64 KB
	// default, which aborted the stream with "token too long".
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var evt anthropicSSEEvent
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		switch evt.Type {
		case "message_start":
			// The prompt side of the accounting — including both cache figures —
			// is only ever reported here. Reading output_tokens alone left every
			// streamed turn billing its cached prefix as fresh input.
			streamUsage = evt.Message.Usage
		case "content_block_start":
			if evt.ContentBlock.Type == "tool_use" {
				toolBlocks[evt.Index] = &streamToolBlock{id: evt.ContentBlock.ID, name: evt.ContentBlock.Name}
			}
		case "content_block_delta":
			switch evt.Delta.Type {
			case "text_delta":
				fullText.WriteString(evt.Delta.Text)
				onToken(evt.Delta.Text)
			case "input_json_delta":
				if tb, ok := toolBlocks[evt.Index]; ok {
					tb.args.WriteString(evt.Delta.PartialJSON)
				}
			}
		case "message_delta":
			// message_delta carries the final output count and, on newer API
			// versions, repeats the cumulative prompt figures. Take each field
			// only when the delta actually reports it, so a version that omits
			// them doesn't zero out what message_start already told us.
			streamUsage.OutputTokens = evt.Usage.OutputTokens
			if evt.Usage.InputTokens > 0 {
				streamUsage.InputTokens = evt.Usage.InputTokens
			}
			if evt.Usage.CacheReadInputTokens > 0 {
				streamUsage.CacheReadInputTokens = evt.Usage.CacheReadInputTokens
			}
			if evt.Usage.CacheCreationInputTokens > 0 {
				streamUsage.CacheCreationInputTokens = evt.Usage.CacheCreationInputTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("stream read: %w", err)
	}

	// Content-block order, not map order. Ranging the map straight out handed
	// the loop a turn's tool calls in a different order on every run, which the
	// agent's repeat guards and the trace both read as a different plan.
	indices := make([]int, 0, len(toolBlocks))
	for idx := range toolBlocks {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var toolCalls []domain.ToolCall
	for _, idx := range indices {
		tb := toolBlocks[idx]
		toolCalls = append(toolCalls, domain.ToolCall{
			ID:   tb.id,
			Type: "function",
			Function: domain.FunctionCall{
				Name:      tb.name,
				Arguments: tb.args.String(),
			},
		})
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   fullText.String(),
			ToolCalls: toolCalls,
		},
		Usage: streamUsage.toDomain(),
	}, nil
}

func (c *anthropicClient) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return knownAnthropicModels, nil
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return knownAnthropicModels, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return knownAnthropicModels, nil
	}
	// Auth errors mean a bad API key — surface them so the user knows.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, httpError(resp, respBody)
	}
	if resp.StatusCode != http.StatusOK {
		return knownAnthropicModels, nil
	}

	var modelsResp modelsResponse
	if err := json.Unmarshal(respBody, &modelsResp); err != nil {
		return knownAnthropicModels, nil
	}
	if len(modelsResp.Data) == 0 {
		return knownAnthropicModels, nil
	}

	models := make([]string, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

func (c *anthropicClient) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, fmt.Errorf("anthropic does not support embeddings")
}
