package context

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Clearing old tool results is the cheapest large saving a long run has and works on every provider. Only the payload goes — the assistant's tool_call block stays as the ledger that stops the re-run doing the search again — but the prefix rewrite invalidates the prompt cache, which is why callers pass a threshold instead of clearing every turn.
func ClearedToolResultNote(toolName string) string {
	// Not an empty string, which would read to the model as a tool that returned nothing.
	if toolName == "" {
		return "[earlier tool output cleared to save context — re-run the call if you need it again]"
	}
	return fmt.Sprintf("[earlier %s output cleared to save context — re-run the call if you need it again]", toolName)
}

// ClearToolResults blanks the payload of every tool result except the most recent keepRecent. Messages are rewritten, never removed: a result pairs with its assistant turn by ToolCallID, and both endpoint families reject a call with no matching result. keepRecent <= 0 clears everything.
func ClearToolResults(messages []domain.Message, keepRecent int) (out []domain.Message, cleared, charsFreed int) {
	// Backwards walk so "recent" counts tool results, not messages.
	protected := make(map[int]struct{}, keepRecent)
	if keepRecent > 0 {
		seen := 0
		for i := len(messages) - 1; i >= 0 && seen < keepRecent; i-- {
			if messages[i].Role != domain.RoleTool {
				continue
			}
			protected[i] = struct{}{}
			seen++
		}
	}

	out = make([]domain.Message, len(messages))
	copy(out, messages)

	for i := range out {
		if out[i].Role != domain.RoleTool {
			continue
		}
		if _, keep := protected[i]; keep {
			continue
		}
		note := ClearedToolResultNote(out[i].Name)
		// The note doubles as a marker: a second pass over the same message must not report savings it did not make.
		if out[i].Content == note && len(out[i].Images) == 0 {
			continue
		}
		charsFreed += len(out[i].Content) - len(note)
		for _, img := range out[i].Images {
			charsFreed += len(img.Data)
		}
		out[i].Content = note
		// A screenshot from thirty turns ago is the least likely thing to still matter, and the largest cost.
		out[i].Images = nil
		cleared++
	}
	return out, cleared, charsFreed
}
