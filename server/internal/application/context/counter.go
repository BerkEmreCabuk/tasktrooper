package context

import (
	"encoding/json"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const charsPerToken = 4

// CountTokens approximates what a history costs as prompt input.
//
// Text is charged by character count, images by estimated pixel area — an
// image carries no characters at all, so counting only text let a handful of
// screenshots blow the budget while the counter reported an almost empty
// request. The two are summed separately: text keeps its single trailing
// ceiling division over the whole history, so text-only counts are unchanged.
func CountTokens(messages []domain.Message) int {
	chars := 0
	images := 0
	for _, m := range messages {
		chars += messageChars(m)
		images += messageImageTokens(m)
	}
	if chars == 0 {
		return images
	}
	return (chars+charsPerToken-1)/charsPerToken + images
}

// CountToolTokens approximates what the tools array costs as prompt input.
//
// Every request also carries the full tool schema catalog — up to ~60 JSON
// tool definitions — riding alongside the messages, and it used to go
// uncounted: CountTokens only ever summed the messages, so every budget
// decision assumed a request smaller than the one actually sent, sometimes by
// thousands of tokens for a large catalog. Each definition is marshaled to
// its own JSON shape (name, description, parameters — see
// domain.ToolDefinition) and charged by the same chars/4 heuristic CountTokens
// applies to message text, so a big tool catalog and a big message history
// are weighed on the same scale. A definition that fails to marshal — none of
// this codebase's callers can produce one, Parameters is always a plain
// JSON-shaped map — is skipped rather than aborting the whole count.
func CountToolTokens(tools []domain.ToolDefinition) int {
	chars := 0
	for _, t := range tools {
		raw, err := json.Marshal(t)
		if err != nil {
			continue
		}
		chars += len(raw)
	}
	if chars == 0 {
		return 0
	}
	return (chars + charsPerToken - 1) / charsPerToken
}

func messageChars(m domain.Message) int {
	n := len(m.Content) + len(m.ToolCallID) + len(m.Name)
	for _, tc := range m.ToolCalls {
		n += len(tc.ID) + len(tc.Type) + len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	return n
}
