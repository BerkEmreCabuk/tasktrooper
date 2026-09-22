package domain

// EmbeddingStatus answers "what is producing this install's embeddings" in one
// object. It exists because the settings page previously computed the answer
// from the provider catalog, which a named llm_endpoints row is deliberately
// absent from — so a workspace embedding happily on its own endpoint was told
// no provider could produce embeddings at all. The catalog cannot answer this
// question; this can.
type EmbeddingStatus struct {
	// Provider is the stored embedding provider, "" when none is pinned and
	// embeddings follow the default chat provider.
	Provider LLMProviderType `json:"provider"`
	// Model is the stored model. Empty when a provider is chosen but no model
	// is pinned or stored for it.
	Model string `json:"model,omitempty"`
	// Dimensions is the vector length when it is known from the model alone
	// (the pin), 0 otherwise — same convention as Service.ResolvedEmbedding.
	Dimensions int `json:"dimensions,omitempty"`
}
