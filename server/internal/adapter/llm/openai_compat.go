package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type openAICompatClient struct {
	baseURL      string
	model        string
	apiKey       string
	extraHeaders map[string]string
	httpClient   *http.Client
}

func NewOpenAICompatClient(baseURL, model, apiKey string, timeout time.Duration) port.LLMClient {
	return newOpenAICompatClientExt(baseURL, model, apiKey, timeout, nil)
}

func newOpenAICompatClientExt(baseURL, model, apiKey string, timeout time.Duration, extraHeaders map[string]string) port.LLMClient {
	// Trim trailing slashes: request paths are joined as baseURL+"/chat/completions"
	// etc., so a pasted URL ending in "/" (e.g. Gemini's ".../v1beta/openai/")
	// would produce a double slash and 404.
	baseURL = strings.TrimRight(baseURL, "/")
	return &openAICompatClient{
		baseURL:      baseURL,
		model:        model,
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

func (c *openAICompatClient) setAuthHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	Tools          []toolDef           `json:"tools,omitempty"`
	ToolChoice     string              `json:"tool_choice,omitempty"`
	Stream         bool                `json:"stream"`
	StreamOptions  *chatStreamOptions  `json:"stream_options,omitempty"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	// MaxTokens caps the completion's own length. omitempty drops it entirely
	// when the caller set none, which is every request this adapter sent
	// before domain.AgentRequest.MaxTokens existed — the endpoint's own
	// default applies exactly as it always has.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// chatStreamOptions asks for the usage block a streamed response otherwise
// never sends. omitempty keeps it off non-streamed requests, which reject it.
type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *chatJSONSchema `json:"json_schema,omitempty"`
}

type chatJSONSchema struct {
	Name   string                 `json:"name"`
	Strict bool                   `json:"strict,omitempty"`
	Schema map[string]interface{} `json:"schema"`
}

// buildResponseFormat maps the provider-neutral request to the OpenAI-compatible
// response_format parameter. Plain JSON mode when no schema is given; schema mode
// otherwise (schema mode needs a name — OpenAI rejects an empty one).
func buildResponseFormat(rf *domain.ResponseFormat) *chatResponseFormat {
	if rf == nil {
		return nil
	}
	if rf.Schema == nil {
		return &chatResponseFormat{Type: domain.ResponseFormatJSONObject}
	}
	name := rf.Name
	if name == "" {
		name = "response"
	}
	return &chatResponseFormat{
		Type:       domain.ResponseFormatJSONSchema,
		JSONSchema: &chatJSONSchema{Name: name, Strict: true, Schema: rf.Schema},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ContentParts is the multimodal form of Content: when it is set, the
	// request sends a content ARRAY instead of the string. It is request-only
	// (responses always come back as a string) and marshalled by
	// MarshalJSON below, never directly — hence the "-" tag.
	ContentParts []chatContentPart `json:"-"`
	ToolCalls    []toolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	Name         string            `json:"name,omitempty"`
}

// chatContentPart is one element of an OpenAI-compatible multimodal content
// array — text, or an image carried as a data: URI.
type chatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

// MarshalJSON keeps content a plain string unless the message actually carries
// parts. Strict OpenAI-compatible servers (and small local ones) reject or
// mishandle an array where they expect a string, so the overwhelmingly common
// text-only message must stay byte-for-byte what it has always been.
func (m chatMessage) MarshalJSON() ([]byte, error) {
	type plain chatMessage // no MarshalJSON of its own — avoids recursing here
	if len(m.ContentParts) == 0 {
		return json.Marshal(plain(m))
	}
	return json.Marshal(struct {
		Role       string            `json:"role"`
		Content    []chatContentPart `json:"content"`
		ToolCalls  []toolCall        `json:"tool_calls,omitempty"`
		ToolCallID string            `json:"tool_call_id,omitempty"`
		Name       string            `json:"name,omitempty"`
	}{
		Role:       m.Role,
		Content:    m.ContentParts,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
		Name:       m.Name,
	})
}

type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// usage follows the OpenAI convention: prompt_tokens ALREADY includes whatever
// the cache served, and prompt_tokens_details.cached_tokens breaks out how much
// of it that was. Endpoints that don't do prompt caching omit the details
// object entirely, which is why it is a pointer — an absent object must read as
// "no cache", not as a zeroed one we then trust.
type usage struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	CompletionTokens    int                  `json:"completion_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

func (u usage) toDomain() domain.Usage {
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	return domain.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      total,
		CacheReadTokens:  cached,
		// OpenAI-compatible endpoints cache automatically and bill the write at
		// the plain input rate, so there is no write premium to record.
		CacheWriteTokens: 0,
	}
}

type modelsResponse struct {
	Data []modelData `json:"data"`
}

type modelData struct {
	ID string `json:"id"`
}

func (c *openAICompatClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	msgs := buildChatMessages(req.Messages)
	tools := buildToolDefs(req.Tools)

	payload := chatRequest{
		Model:          model,
		Messages:       msgs,
		Stream:         false,
		ResponseFormat: buildResponseFormat(req.ResponseFormat),
		MaxTokens:      req.MaxTokens,
	}
	applyTools(&payload, tools)

	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(httpReq)

	log.Debug().Str("model", model).Int("messages", len(msgs)).Int("tools", len(tools)).Msg("sending chat request")

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

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return domain.AgentResponse{}, fmt.Errorf("llm returned no choices")
	}

	if chatResp.Usage.TotalTokens > 0 {
		log.Debug().
			Int("prompt_tokens", chatResp.Usage.PromptTokens).
			Int("completion_tokens", chatResp.Usage.CompletionTokens).
			Int("total_tokens", chatResp.Usage.TotalTokens).
			Msg("llm usage")
	}

	cm := chatResp.Choices[0].Message
	msg := domain.Message{
		Role:    domain.Role(cm.Role),
		Content: cm.Content,
	}
	msg.ToolCalls = parseToolCalls(cm.ToolCalls)

	return domain.AgentResponse{
		Message: msg,
		Usage:   chatResp.Usage.toDomain(),
	}, nil
}

// buildToolDefs renders the provider-neutral tool definitions for an
// OpenAI-compatible endpoint. Shared by Chat and ChatStream: a streamed turn
// plans its tool calls exactly like a buffered one, and a stream sent without
// tools can only ever answer in prose.
func buildToolDefs(tools []domain.ToolDefinition) []toolDef {
	out := make([]toolDef, 0, len(tools))
	for _, t := range tools {
		out = append(out, toolDef{
			Type: t.Type,
			Function: functionDef{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}
	return out
}

func applyTools(payload *chatRequest, tools []toolDef) {
	if len(tools) == 0 {
		return
	}
	payload.Tools = tools
	// Some local runtimes only emit tool calls when tool_choice is explicit.
	payload.ToolChoice = "auto"
}

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
	Usage   *usage         `json:"usage,omitempty"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type streamDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

// streamToolCall is one fragment of a tool call. A provider sends the id and
// the function name once and then dribbles the arguments out across as many
// chunks as it likes, all tied together by index.
//
// Index is a pointer because a missing index and index 0 are different things:
// servers that send no index at all put every call of a turn at 0, which would
// splice two calls' arguments into one unparseable string. See toolCallStream.
type streamToolCall struct {
	Index    *int                `json:"index,omitempty"`
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function *streamFunctionCall `json:"function,omitempty"`
}

type streamFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// toolCallAccum is one call under construction. It is only ever held by
// pointer: a strings.Builder panics if it is copied after its first write, and
// a slice of them is copied wholesale every time it grows.
type toolCallAccum struct {
	call toolCall
	args strings.Builder
}

// toolCallStream reassembles the fragments into whole calls, in the order the
// provider first mentioned them.
type toolCallStream struct {
	order []*toolCallAccum
	byKey map[int]*toolCallAccum
}

func newToolCallStream() *toolCallStream {
	return &toolCallStream{byKey: make(map[int]*toolCallAccum)}
}

// add folds one fragment in. pos is the fragment's place in its own chunk, used
// as the key when the provider sends no index.
func (s *toolCallStream) add(pos int, frag streamToolCall) {
	key := pos
	if frag.Index != nil {
		key = *frag.Index
	}

	acc, known := s.byKey[key]
	// A fresh id under a key that already carries a different one means the
	// provider is numbering nothing and has simply moved on to the next call.
	if known && frag.ID != "" && acc.call.ID != "" && acc.call.ID != frag.ID {
		known = false
	}
	if !known {
		acc = &toolCallAccum{call: toolCall{Type: "function"}}
		s.order = append(s.order, acc)
		s.byKey[key] = acc
	}

	if frag.ID != "" {
		acc.call.ID = frag.ID
	}
	if frag.Type != "" {
		acc.call.Type = frag.Type
	}
	if frag.Function != nil {
		if frag.Function.Name != "" {
			acc.call.Function.Name = frag.Function.Name
		}
		acc.args.WriteString(frag.Function.Arguments)
	}
}

// calls returns the finished calls in the wire shape the buffered path produces,
// so both go through parseToolCalls and get the same id repair.
func (s *toolCallStream) calls() []toolCall {
	out := make([]toolCall, 0, len(s.order))
	for _, acc := range s.order {
		c := acc.call
		c.Function.Arguments = acc.args.String()
		out = append(out, c)
	}
	return out
}

func (c *openAICompatClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	model := c.model
	if req.Model != "" && req.Model != "local" {
		model = req.Model
	}

	msgs := buildChatMessages(req.Messages)
	tools := buildToolDefs(req.Tools)

	payload := chatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   true,
		// Without this a streamed response carries no usage at all, so every
		// streamed turn was billed as zero tokens. Providers that do not know
		// the option ignore it and simply send no usage, which is what the
		// nil check below already handles.
		StreamOptions:  &chatStreamOptions{IncludeUsage: true},
		ResponseFormat: buildResponseFormat(req.ResponseFormat),
		MaxTokens:      req.MaxTokens,
	}
	applyTools(&payload, tools)

	body, err := json.Marshal(payload)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	c.setAuthHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return domain.AgentResponse{}, httpError(resp, body)
	}

	var fullContent strings.Builder
	toolCalls := newToolCallStream()
	var streamUsage usage

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
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// The usage chunk is read before the choices check, not after: the
		// chunk that carries usage is the one with an EMPTY choices array, so
		// skipping it as choiceless threw away the only token count sent.
		if chunk.Usage != nil {
			streamUsage = *chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		// Only text goes to the caller. Tool-call fragments are half-written
		// JSON — forwarding them would print the model's plumbing into the
		// user's answer.
		if delta.Content != "" {
			fullContent.WriteString(delta.Content)
			onToken(delta.Content)
		}
		for pos, frag := range delta.ToolCalls {
			toolCalls.add(pos, frag)
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.AgentResponse{}, fmt.Errorf("stream read: %w", err)
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   fullContent.String(),
			ToolCalls: parseToolCalls(toolCalls.calls()),
		},
		Usage: streamUsage.toDomain(),
	}, nil
}

func (c *openAICompatClient) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp, respBody)
	}

	var modelsResp modelsResponse
	if err := json.Unmarshal(respBody, &modelsResp); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]string, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

// llmErrorBodyMax bounds how much of a provider's rejection body this type
// carries; it is quoted into tool results, run summaries and logs.
const llmErrorBodyMax = 2000

// EmbeddingUnavailableError is the embedding provider refusing the call for a
// reason that sending the same bytes again cannot fix: a key it does not
// accept, a plan that is unpaid or spent, a model it will not serve, or a
// window it has shut. It also covers never reaching the provider at all.
//
// It exists because callers could only see the provider's own words. The tool
// that reads this error surfaced `embeddings returned 402: {"detail":"Check
// your subscription on https://admin.mistral.ai/subscription"}` verbatim, which
// the agent read as a transient glitch and retried eight times in one run. A
// type lets that caller say "this server has no semantic search, use the other
// tools" without matching on a provider's prose.
type EmbeddingUnavailableError struct {
	// StatusCode is the provider's refusal status, or 0 when the request never
	// got an answer (DNS, dial, TLS, timeout) — Cause holds the reason then.
	StatusCode int
	Body       string
	Cause      error
}

func (e *EmbeddingUnavailableError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("embeddings unreachable: %v", e.Cause)
	}
	// The provider's own wording is kept so logs and stored run summaries read
	// the same as they did before this type existed.
	return fmt.Sprintf("embeddings returned %d: %s", e.StatusCode, e.Body)
}

func (e *EmbeddingUnavailableError) Unwrap() error { return e.Cause }

// embeddingRejectionStatuses are the refusals no retry can clear: the account
// is not authenticated (401), not paid up or out of credit (402), or not
// allowed this model (403). 429 is deliberately absent — it is retryable and
// stays a *RateLimitError so the account limiter can honour its Retry-After.
var embeddingRejectionStatuses = map[int]bool{
	http.StatusUnauthorized:    true,
	http.StatusPaymentRequired: true,
	http.StatusForbidden:       true,
}

// newEmbeddingUnavailableError types a refusal the caller must not retry, and
// returns nil for any other status so the plain error is used instead.
func newEmbeddingUnavailableError(statusCode int, body string) *EmbeddingUnavailableError {
	if !embeddingRejectionStatuses[statusCode] {
		return nil
	}
	return &EmbeddingUnavailableError{
		StatusCode: statusCode,
		Body:       domain.TruncateHead(body, llmErrorBodyMax),
	}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *openAICompatClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	embedModel := model
	if embedModel == "" {
		embedModel = c.model
	}

	payload := embedRequest{Model: embedModel, Input: input}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// A call that never reached the provider is typed too, but not when the
		// caller is the one who walked away: a cancelled or expired context is
		// our own doing and must not be reported as the provider being down.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("http request: %w", err)
		}
		return nil, &EmbeddingUnavailableError{Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// A 429 is retryable and carries the provider's own pacing hint, so it
		// leaves this client as a typed error instead of an opaque string.
		if rle := newRateLimitError("embeddings", resp, string(respBody)); rle != nil {
			return nil, rle
		}
		// An auth, billing or permission refusal is the opposite: retrying it
		// spends the run to collect the same answer, so it is typed as such.
		if eue := newEmbeddingUnavailableError(resp.StatusCode, string(respBody)); eue != nil {
			return nil, eue
		}
		return nil, fmt.Errorf("embeddings returned %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal embed response: %w", err)
	}

	if len(embedResp.Data) == 0 || len(embedResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings returned empty data")
	}

	return embedResp.Data[0].Embedding, nil
}
