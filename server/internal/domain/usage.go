package domain

import "time"

// LLMUsageRecord is one LLM call's token usage. The cache fields are subsets of
// PromptTokens, never additions to it — see Usage for the full contract.
type LLMUsageRecord struct {
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CacheReadTokens  int       `json:"cache_read_tokens"`
	CacheWriteTokens int       `json:"cache_write_tokens"`
	CreatedAt        time.Time `json:"created_at"`
}

// LLMUsageTotals aggregates token counts over a group (day, model, or all).
type LLMUsageTotals struct {
	Calls            int `json:"calls"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

type LLMUsageByModel struct {
	Model string `json:"model"`
	LLMUsageTotals
}

type LLMUsageByDay struct {
	Day string `json:"day"` // YYYY-MM-DD (UTC)
	LLMUsageTotals
}

// LLMUsageSummary is the dashboard payload for the last N days.
type LLMUsageSummary struct {
	Days    int               `json:"days"`
	Total   LLMUsageTotals    `json:"total"`
	ByModel []LLMUsageByModel `json:"by_model"`
	Daily   []LLMUsageByDay   `json:"daily"`
}
