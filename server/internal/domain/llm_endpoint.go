package domain

import "time"

// LLMEndpoint is a named, user-configured OpenAI-compatible endpoint (own IP,
// Ollama, OpenRouter, Groq, vLLM, LM Studio, …). Its ID (uuid) doubles as a
// provider ref: it can be stored in active_llm_provider / embedding_llm_provider
// / agents.provider_type and reuses the string-keyed client dispatch. The
// OpenAI-compatible client is built by the llm factory's default case.
type LLMEndpoint struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	BaseURL        string    `json:"base_url"`
	DefaultModel   string    `json:"default_model"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Configured     bool      `json:"configured"`
	HasAPIKey      bool      `json:"has_api_key"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SaveLLMEndpointRequest is the create/update payload. On update an empty APIKey
// keeps the stored key; on create it sets it. Model is optional (chosen per agent).
type SaveLLMEndpointRequest struct {
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	DefaultModel   string `json:"default_model"`
	APIKey         string `json:"api_key"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}
