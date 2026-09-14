package llm

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func NewProviderClient(providerType domain.LLMProviderType, baseURL, model, apiKey string, timeout time.Duration) port.LLMClient {
	switch providerType {
	case domain.LLMProviderAnthropic:
		return NewAnthropicClient(baseURL, model, apiKey, timeout)
	case domain.LLMProviderGemini:
		project := os.Getenv("GOOGLE_CLOUD_PROJECT")
		location := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if location == "" {
			location = "global"
		}
		c, err := NewGeminiVertexClient(project, location, model, apiKey, timeout)
		if err != nil {
			return &errorLLMClient{fmt.Errorf("gemini: %w (AI Studio with an API key; for keyless Vertex set GOOGLE_CLOUD_PROJECT + gcloud auth application-default login)", err)}
		}
		return c
	case domain.LLMProviderLocalRunner:
		// Unreachable in practice: this provider only ever gets an entry when a
		// control plane is wired, and there is none. A client that fails with a
		// sentence still beats one that dials an empty base URL and reports a
		// connection error.
		return &errorLLMClient{fmt.Errorf(
			"%s embeddings would need another machine to reach; set EMBEDDINGS_BASE_URL "+
				"to a local OpenAI-compatible embedder instead", domain.LLMProviderLocalRunner)}
	default:
		return NewOpenAICompatClient(baseURL, model, apiKey, timeout)
	}
}

type errorLLMClient struct{ err error }

func (e *errorLLMClient) Chat(_ context.Context, _ domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, e.err
}
func (e *errorLLMClient) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, e.err
}
func (e *errorLLMClient) Models(_ context.Context) ([]string, error) { return nil, e.err }
func (e *errorLLMClient) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, e.err
}
