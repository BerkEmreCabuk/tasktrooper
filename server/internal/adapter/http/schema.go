package http

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

type chatCompletionRequest struct {
	Model      string            `json:"model"`
	Messages   []requestMessage  `json:"messages"`
	Stream     bool              `json:"stream"`
	SessionID  string            `json:"session_id,omitempty"`
	ToolPolicy domain.ToolPolicy `json:"tool_policy,omitempty"`
	FileIDs    []string          `json:"file_ids,omitempty"`
}

type requestMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
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

type chatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Index        int             `json:"index"`
	Message      responseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type responseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type modelsResponse struct {
	Object string      `json:"object"`
	Data   []modelData `json:"data"`
}

type modelData struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	// Label is the human-readable name for a picker. Empty (and omitted) for
	// providers whose model list is fetched from the endpoint, where the id IS
	// the name; set for the curated lists — see domain.ClaudeCodeModels, whose
	// ids are routing aliases nobody would recognise on sight and one of which
	// is the empty string.
	Label string `json:"label,omitempty"`
}

type toolsResponse struct {
	Tools []domain.ToolDefinition `json:"tools"`
	Count int                     `json:"count"`
}

type healthResponse struct {
	Status    string                  `json:"status"`
	LLM       string                  `json:"llm"`
	Providers []llmProviderHealthItem `json:"providers,omitempty"`
}

type llmProviderHealthItem struct {
	ProviderType string `json:"provider_type"`
	Label        string `json:"label"`
	Configured   bool   `json:"configured"`
	Active       bool   `json:"active"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
}

type streamChunkEvent struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []streamChoiceEvent `json:"choices"`
	// Set only on the frame that reports a failed run, and additive on purpose:
	// the same message is still repeated in the delta below, and the frame order
	// (chunk, then [DONE]) is unchanged, so a client that decodes a fixed struct
	// and drops unknown keys — the iOS SSEStreamParser does exactly that — is
	// untouched. Without it a failure is indistinguishable from agent output,
	// which is how error text ended up rendered as the assistant's own reply.
	Error *errorDetail `json:"error,omitempty"`
}

type streamChoiceEvent struct {
	Index        int              `json:"index"`
	Delta        streamDeltaEvent `json:"delta"`
	FinishReason string           `json:"finish_reason,omitempty"`
}

type streamDeltaEvent struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// Phase marks a frame that carries no text but says something about the
	// stream's shape. The only value is "reasoning_end": everything streamed up
	// to here was the model's reasoning ahead of a tool call, not the answer.
	// Omitted on every ordinary frame, so a client that ignores it sees exactly
	// the stream it always saw — which is what leaves the iOS decoder (fixed
	// struct, unknown keys dropped) untouched.
	Phase string `json:"phase,omitempty"`
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}
