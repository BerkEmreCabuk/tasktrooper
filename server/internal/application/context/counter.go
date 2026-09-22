package context

import (
	"encoding/json"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const charsPerToken = 4

// CountTokens approximates what a history costs as prompt input. Images carry no characters, so they are charged by pixel area — counting text alone let a few screenshots blow the budget while the counter reported an almost empty request.
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

// CountToolTokens approximates what the tool schema catalog costs: it rides on every request but used to go uncounted, so budgets assumed a request thousands of tokens smaller than the one actually sent.
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
