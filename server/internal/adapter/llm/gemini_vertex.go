package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var knownGeminiModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-pro",
	"gemini-2.0-flash",
	"gemini-2.0-flash-lite",
	"gemini-1.5-pro",
	"gemini-1.5-flash",
}

type geminiVertexClient struct {
	client *genai.Client
	model  string
}

func NewGeminiVertexClient(project, location, model, apiKey string, _ time.Duration) (port.LLMClient, error) {
	var cfg *genai.ClientConfig
	if apiKey != "" {
		// API key → Gemini API (generativelanguage.googleapis.com), no project/location needed
		cfg = &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		}
	} else {
		// No API key → Vertex AI (aiplatform.googleapis.com) with ADC
		cfg = &genai.ClientConfig{
			Project:  project,
			Location: location,
			Backend:  genai.BackendVertexAI,
		}
	}
	client, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("create vertex ai client: %w", err)
	}
	return &geminiVertexClient{client: client, model: model}, nil
}

func (c *geminiVertexClient) modelID(req domain.AgentRequest) string {
	if req.Model != "" && req.Model != "local" {
		return req.Model
	}
	return c.model
}

// geminiInlineImageParts turns the domain's base64 images into inline_data
// parts. The SDK's Blob takes raw bytes and re-encodes them itself, so an image
// whose data is not valid base64 is dropped rather than shipped as garbage the
// API would reject for the whole request.
func geminiInlineImageParts(images []domain.ToolResultImage) []*genai.Part {
	if len(images) == 0 {
		return nil
	}
	parts := make([]*genai.Part, 0, len(images))
	for _, img := range images {
		raw, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			continue
		}
		parts = append(parts, &genai.Part{
			InlineData: &genai.Blob{MIMEType: img.MediaType, Data: raw},
		})
	}
	return parts
}

func buildGeminiContents(messages []domain.Message) ([]*genai.Content, *genai.Content) {
	var contents []*genai.Content
	// Gemini takes one system instruction, but callers stack several system
	// messages (persona + skills + rules, KPI, memory, workspace note, language
	// rule). Collect them all — overwriting would drop the agent's instructions.
	//
	// Only the leading run of them, though: Gemini has no system role inside the
	// contents array either, so a mid-run injection takes the same in-position
	// user turn Anthropic gives it. See systemReminder.
	var systemParts []string
	seenTurn := false

	for i := 0; i < len(messages); {
		m := messages[i]
		switch m.Role {
		case domain.RoleSystem:
			if strings.TrimSpace(m.Content) != "" {
				if seenTurn {
					contents = append(contents, &genai.Content{
						Role:  genai.RoleUser,
						Parts: []*genai.Part{{Text: systemReminder(m.Content)}},
					})
				} else {
					systemParts = append(systemParts, m.Content)
				}
			}
			i++
		case domain.RoleUser:
			seenTurn = true
			// A lone text part is what every user turn has always been; images
			// the human attached ride as inline_data parts behind it, which is
			// how Gemini takes multimodal input.
			parts := []*genai.Part{{Text: m.Content}}
			parts = append(parts, geminiInlineImageParts(m.Images)...)
			contents = append(contents, &genai.Content{
				Role:  genai.RoleUser,
				Parts: parts,
			})
			i++
		case domain.RoleAssistant:
			seenTurn = true
			var parts []*genai.Part
			if m.Content != "" {
				parts = append(parts, &genai.Part{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				parts = append(parts, &genai.Part{
					FunctionCall: &genai.FunctionCall{
						ID:   tc.ID,
						Name: tc.Function.Name,
						Args: args,
					},
				})
			}
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{
					Role:  genai.RoleModel,
					Parts: parts,
				})
			}
			i++
		case domain.RoleTool:
			seenTurn = true
			// batch consecutive tool results into one user turn
			var parts []*genai.Part
			var carried []domain.ToolResultImage
			for i < len(messages) && messages[i].Role == domain.RoleTool {
				tm := messages[i]
				var response map[string]any
				if err := json.Unmarshal([]byte(tm.Content), &response); err != nil {
					response = map[string]any{"result": tm.Content}
				}
				// FunctionResponse is a JSON map, not content parts — images
				// cannot ride along with the result itself, so they follow the
				// batch as inline data on their own user turn.
				if len(tm.Images) > 0 {
					if response == nil {
						response = map[string]any{}
					}
					response["images_note"] = strings.TrimSpace(carriedImagesNote(len(tm.Images)))
					carried = append(carried, tm.Images...)
				}
				parts = append(parts, &genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						ID:       tm.ToolCallID,
						Name:     tm.Name,
						Response: response,
					},
				})
				i++
			}
			contents = append(contents, &genai.Content{
				Role:  genai.RoleUser,
				Parts: parts,
			})
			if imageParts := geminiInlineImageParts(carried); len(imageParts) > 0 {
				contents = append(contents, &genai.Content{
					Role: genai.RoleUser,
					Parts: append([]*genai.Part{{Text: toolImagePreamble(len(imageParts))}},
						imageParts...),
				})
			}
		default:
			i++
		}
	}

	if len(systemParts) == 0 {
		return contents, nil
	}
	return contents, &genai.Content{
		Parts: []*genai.Part{{Text: strings.Join(systemParts, "\n\n")}},
	}
}

func buildGeminiConfig(systemInstruction *genai.Content, tools []domain.ToolDefinition, respFormat *domain.ResponseFormat, maxOutputTokens int32) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
	}
	if maxOutputTokens > 0 {
		config.MaxOutputTokens = maxOutputTokens
	}
	if len(tools) > 0 {
		decls := make([]*genai.FunctionDeclaration, 0, len(tools))
		for _, t := range tools {
			decls = append(decls, &genai.FunctionDeclaration{
				Name:                 t.Function.Name,
				Description:          t.Function.Description,
				ParametersJsonSchema: t.Function.Parameters,
			})
		}
		config.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
	} else if respFormat != nil {
		// Gemini rejects a JSON response MIME type combined with function
		// declarations, so JSON mode only applies to tool-less requests.
		config.ResponseMIMEType = "application/json"
		if respFormat.Schema != nil {
			config.ResponseJsonSchema = respFormat.Schema
		}
	}
	return config
}

// geminiToolCalls converts the SDK's function calls to the domain shape. The
// streamed path needs the same conversion the buffered one does — a chunk
// carries whole function calls, not fragments, so it can simply collect them.
func geminiToolCalls(fcs []*genai.FunctionCall) []domain.ToolCall {
	toolCalls := make([]domain.ToolCall, 0, len(fcs))
	for _, fc := range fcs {
		argsJSON, _ := json.Marshal(fc.Args)
		id := fc.ID
		if id == "" {
			id = fc.Name
		}
		toolCalls = append(toolCalls, domain.ToolCall{
			ID:   id,
			Type: "function",
			Function: domain.FunctionCall{
				Name:      fc.Name,
				Arguments: string(argsJSON),
			},
		})
	}
	if len(toolCalls) == 0 {
		return nil
	}
	return toolCalls
}

// geminiUsage normalizes Vertex's accounting. PromptTokenCount is already the
// whole prompt with the cached span folded in, matching domain.Usage, so only
// the cached slice needs breaking out. Gemini has no write premium to report:
// context caching is provisioned ahead of a call rather than billed per
// request, so CacheWriteTokens stays zero.
func geminiUsage(m *genai.GenerateContentResponseUsageMetadata) domain.Usage {
	return domain.Usage{
		PromptTokens:     int(m.PromptTokenCount),
		CompletionTokens: int(m.CandidatesTokenCount),
		TotalTokens:      int(m.TotalTokenCount),
		CacheReadTokens:  int(m.CachedContentTokenCount),
	}
}

func parseGeminiResponse(resp *genai.GenerateContentResponse) domain.AgentResponse {
	toolCalls := geminiToolCalls(resp.FunctionCalls())

	var usage domain.Usage
	if m := resp.UsageMetadata; m != nil {
		usage = geminiUsage(m)
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   resp.Text(),
			ToolCalls: toolCalls,
		},
		Usage: usage,
	}
}

func (c *geminiVertexClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	contents, sysInstr := buildGeminiContents(req.Messages)
	config := buildGeminiConfig(sysInstr, req.Tools, req.ResponseFormat, int32(req.MaxTokens))

	resp, err := c.client.Models.GenerateContent(ctx, c.modelID(req), contents, config)
	if err != nil {
		return domain.AgentResponse{}, fmt.Errorf("gemini generate content: %w", err)
	}

	return parseGeminiResponse(resp), nil
}

func (c *geminiVertexClient) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	contents, sysInstr := buildGeminiContents(req.Messages)
	config := buildGeminiConfig(sysInstr, req.Tools, req.ResponseFormat, int32(req.MaxTokens))

	var fullText strings.Builder
	var toolCalls []domain.ToolCall
	var finalUsage domain.Usage

	for resp, err := range c.client.Models.GenerateContentStream(ctx, c.modelID(req), contents, config) {
		if err != nil {
			return domain.AgentResponse{}, fmt.Errorf("gemini stream: %w", err)
		}
		// Only the text reaches the caller; a function call is plumbing, not
		// something to print into the answer.
		if text := resp.Text(); text != "" {
			fullText.WriteString(text)
			onToken(text)
		}
		// Every streamed turn used to come back tool-less, so a streamed run
		// could only ever answer in prose no matter what the model asked for.
		toolCalls = append(toolCalls, geminiToolCalls(resp.FunctionCalls())...)
		if m := resp.UsageMetadata; m != nil && m.TotalTokenCount > 0 {
			finalUsage = geminiUsage(m)
		}
	}

	return domain.AgentResponse{
		Message: domain.Message{
			Role:      domain.RoleAssistant,
			Content:   fullText.String(),
			ToolCalls: toolCalls,
		},
		Usage: finalUsage,
	}, nil
}

func (c *geminiVertexClient) Models(_ context.Context) ([]string, error) {
	return knownGeminiModels, nil
}

func (c *geminiVertexClient) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	embedModel := strings.TrimPrefix(strings.TrimSpace(model), "models/")
	// text-embedding-004 API'den kaldırıldı; eski kayıtları da (eski "models/" önekiyle
	// kaydedilmiş olsa dahi) yeni modele çevir.
	if embedModel == "" || !strings.Contains(embedModel, "embedding") || embedModel == "text-embedding-004" {
		embedModel = "gemini-embedding-001"
	}
	contents := []*genai.Content{{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: input}},
	}}
	resp, err := c.client.Models.EmbedContent(ctx, embedModel, contents, nil)
	if err != nil {
		return nil, geminiEmbedError(ctx, err)
	}
	if len(resp.Embeddings) == 0 || len(resp.Embeddings[0].Values) == 0 {
		return nil, fmt.Errorf("gemini embed returned empty result")
	}
	return resp.Embeddings[0].Values, nil
}

// geminiEmbedError classifies an embedding failure the same way the
// OpenAI-compatible client does, so codebase_search can word it the same way.
//
// It exists because the two clients failed differently for the same reason: a
// rejected Gemini/Vertex key got `gemini embed: Error 403, Message: ...` —
// the provider's own prose, which the agent reads as worth another go —
// while the same failure on Mistral got a typed error and a tool result that
// told it to stop. The classification belongs to the failure, not to the SDK
// that happened to report it.
//
// Anything else (a malformed request, an SDK-level fault) keeps the original
// wrapping: it is a bug here, not a provider verdict, and typing it would tell
// the agent to abandon semantic search over our own mistake.
func geminiEmbedError(ctx context.Context, err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		body := strings.TrimSpace(apiErr.Message)
		if body == "" {
			body = apiErr.Status
		}
		// 429/503 stay retryable and carry the provider's own wait, exactly as
		// newRateLimitError builds them on the OpenAI-compatible path.
		if apiErr.Code == http.StatusTooManyRequests || apiErr.Code == http.StatusServiceUnavailable {
			return &RateLimitError{
				Endpoint:   "embeddings",
				StatusCode: apiErr.Code,
				RetryAfter: geminiRetryDelay(apiErr.Details),
				Body:       domain.TruncateHead(body, llmErrorBodyMax),
			}
		}
		if eue := newEmbeddingUnavailableError(apiErr.Code, body); eue != nil {
			return eue
		}
		return fmt.Errorf("gemini embed: %w", err)
	}
	// A request that never got an answer is typed too — but not when we are the
	// ones who walked away, since a cancelled context is our doing.
	var urlErr *url.Error
	if errors.As(err, &urlErr) && ctx.Err() == nil {
		return &EmbeddingUnavailableError{Cause: err}
	}
	return fmt.Errorf("gemini embed: %w", err)
}

// geminiRetryDelay reads the wait out of a google.rpc.RetryInfo detail, which
// is where Gemini and Vertex state it — the SDK discards response headers, so
// Retry-After never reaches us on this path.
func geminiRetryDelay(details []map[string]any) time.Duration {
	for _, detail := range details {
		delay, _ := detail["retryDelay"].(string)
		if d := parseResetHint(delay); d > 0 {
			return d
		}
	}
	return 0
}
