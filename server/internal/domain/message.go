package domain

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	// Images carries pictures the model should see: tool-result screenshots on
	// a RoleTool message, and the image attachments the human sent on a
	// RoleUser one. Every provider renders user images natively; a provider
	// whose TOOL messages are text-only must drop those with a visible note,
	// never silently.
	Images []ToolResultImage `json:"images,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolResultImage is one image attached to a tool result — e.g. a browser
// screenshot the model must be able to see. Data is base64 without a data: URI
// prefix; MediaType is the MIME type (e.g. "image/png").
type ToolResultImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type ToolResult struct {
	ToolCallID    string                `json:"tool_call_id"`
	Name          string                `json:"name"`
	Content       string                `json:"content"`
	IsError       bool                  `json:"is_error"`
	Images        []ToolResultImage     `json:"images,omitempty"`
	Clarification *ClarificationRequest `json:"clarification,omitempty"`
	// ResourceBlock stops the turn the way Clarification does, but waits on a
	// contended resource rather than on a human. See resource_block.go.
	ResourceBlock *ResourceBlock `json:"resource_block,omitempty"`
}

type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ResponseFormat asks the provider to constrain its output shape. Type
// "json_object" forces valid JSON; "json_schema" additionally validates
// against Schema. Providers that lack a native equivalent degrade to a
// system-prompt instruction rather than failing the request.
type ResponseFormat struct {
	Type   string                 `json:"type"`
	Name   string                 `json:"name,omitempty"`
	Schema map[string]interface{} `json:"schema,omitempty"`
}

const (
	ResponseFormatJSONObject = "json_object"
	ResponseFormatJSONSchema = "json_schema"
)

// JSONResponseFormat is the plain JSON mode used by pipeline stages whose
// output is parsed with json.Unmarshal (planner, intake, verifier, ...).
func JSONResponseFormat() *ResponseFormat {
	return &ResponseFormat{Type: ResponseFormatJSONObject}
}

// JSONSchemaResponseFormat is strict-schema JSON mode: on a provider that
// supports constrained decoding (the OpenAI-compatible adapter's
// json_schema+strict, Gemini's ResponseJsonSchema, Anthropic's output_config)
// the response is guaranteed to match schema, which is what lets a pipeline
// stage retire its expensive parse-repair retry loop on that provider. name
// identifies the schema — some providers reject an empty one, so callers
// should pass something stable and descriptive (e.g. "planner_output").
// schema is a JSON Schema object (type/properties/required/additionalProperties);
// see internal/application/orchestrator/schemas.go for the pipeline's.
func JSONSchemaResponseFormat(name string, schema map[string]interface{}) *ResponseFormat {
	return &ResponseFormat{Type: ResponseFormatJSONSchema, Name: name, Schema: schema}
}

type AgentRequest struct {
	Messages       []Message
	Tools          []ToolDefinition
	ProviderType   LLMProviderType
	Model          string
	ToolPolicy     ToolPolicy
	ResponseFormat *ResponseFormat
	// CacheAnchorIndex marks the end of the byte-stable prefix in Messages:
	// Messages[:CacheAnchorIndex] is the part a run promises not to rewrite
	// (the agent loop's run-start head). Providers that support explicit
	// prompt-cache breakpoints put one at that boundary. Zero means "no
	// anchor" — a caller that doesn't track a stable head gets the provider's
	// default placement, never a wrong one. Transient: it rides with the
	// request and is never persisted.
	CacheAnchorIndex int
	// MaxTokens caps the model's own output for this request. Zero means "the
	// adapter's existing default behaviour": Anthropic keeps its 8192/4096
	// per-model cap, the OpenAI-compatible and Gemini adapters omit the field
	// and let the provider decide. Pipeline stages whose output is a JSON
	// schema (planner, intake, verifier, replanner, evolution) deliberately
	// leave this at 0 — that output can legitimately be long. The agent loop
	// sets it from its own context budget's output reserve, and small utility
	// calls (history summarization, commit message rewrites) set their own
	// small caps.
	MaxTokens int
	// Effort is how hard the model should think before answering, resolved from
	// the agent record. Empty means "the adapter's own default", the same
	// convention MaxTokens uses above with 0.
	//
	// It is the same knob the claude_code executor passes as --effort, reaching
	// an HTTP provider as output_config.effort instead. An adapter whose
	// provider has no equivalent drops it, exactly as it drops a MaxTokens it
	// cannot express.
	Effort string
	// ClearToolResults asks the provider to drop old tool RESULTS from the
	// context server-side, keeping the tool_use blocks that record what was
	// called. Off unless a caller sets it, and deliberately so.
	//
	// The loop already trims client-side (fitHistory + context.Budget), and the
	// two do not compose for free: server-side clearing rewrites the prefix,
	// which invalidates the prompt cache from the edit point — the same prefix
	// application/context/stable.go exists to hold still. Turning this on trades
	// a cache write for a smaller history, and pays only when the history is
	// large enough that carrying it costs more than re-establishing the cache.
	ClearToolResults bool
}

type AgentResponse struct {
	Message       Message               `json:"message"`
	Usage         Usage                 `json:"usage"`
	Clarification *ClarificationRequest `json:"clarification,omitempty"`
	// ResourceBlock is set when the run stopped because a shared resource was
	// held by someone else. Like Clarification it means "this run produced no
	// deliverable" — no verification, no commit, no hand-off.
	ResourceBlock *ResourceBlock `json:"resource_block,omitempty"`
	// Verification is the orchestration verifier's verdict on the run that
	// produced this response, when one ran. Nil means nobody judged it — the run
	// was not orchestrated, verification is off, or the verifier never returned a
	// verdict — and nil must never be read as a pass.
	//
	// It rides on the response because the verdict used to reach nothing outside
	// the plan row: the plan was stamped incomplete while the board hand-off,
	// which reads only this response, promoted the task to code_review anyway. A
	// card whose own verification panel reads FAILED sitting in review is that gap.
	Verification *VerificationResult `json:"verification,omitempty"`
}

// Usage is one call's token accounting, normalized across providers.
//
// The contract every adapter must satisfy:
//
//   - PromptTokens is the TOTAL prompt size, cached tokens included.
//   - CacheReadTokens ⊆ PromptTokens — the part served from cache (cheap).
//   - CacheWriteTokens ⊆ PromptTokens — the part written to cache (a premium
//     over the base rate on providers that charge for the write).
//   - PromptTokens - CacheReadTokens - CacheWriteTokens is what was processed
//     at the plain input rate.
//
// Providers disagree on this natively and each adapter converts: Anthropic
// reports input_tokens EXCLUDING both cache figures (so the adapter adds them
// back), while OpenAI-compatible endpoints report prompt_tokens INCLUDING the
// cached ones (so the adapter passes it through). Billing does arithmetic on
// these fields, so a provider that splits them differently must be normalized
// here rather than compensated for downstream.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}
