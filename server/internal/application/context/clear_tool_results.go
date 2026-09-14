package context

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Clearing old tool RESULTS is the cheapest large saving available to a long
// run, and unlike every provider's own version of it, this one works on all of
// them.
//
// The shape of the cost is why it pays. The API is stateless, so every turn
// resends the whole transcript: a run's input tokens grow with the SQUARE of
// its turns, and the largest term in that sum is tool output — a directory
// listing, a test log, a file read — carried forward long after the turn that
// needed it. The model has already taken what it wanted; what remains is bytes.
//
// What survives is deliberate. The assistant's tool_call block stays, so the
// run keeps its ledger of what it already did and does not re-run the same
// search; only the payload goes. That is the same split Anthropic's server-side
// clear_tool_uses makes, done here so a Mistral or a local endpoint gets it too.
//
// Two costs, both real. Clearing rewrites the prefix, so the prompt cache is
// invalidated from the first cleared message — worth it once the cleared bytes
// exceed what re-establishing the cache costs, which is why callers pass a
// threshold rather than clearing every turn. And a model that genuinely needs a
// cleared payload must read it again; keepRecent exists so the results it is
// actively working with are never the ones taken.

// ClearedToolResultNote replaces a cleared payload. It says what happened and
// what to do about it, because the alternative — an empty string — reads to the
// model as a tool that returned nothing, which is a different and misleading
// fact. Naming the tool keeps the ledger readable.
func ClearedToolResultNote(toolName string) string {
	if toolName == "" {
		return "[earlier tool output cleared to save context — re-run the call if you need it again]"
	}
	return fmt.Sprintf("[earlier %s output cleared to save context — re-run the call if you need it again]", toolName)
}

// ClearToolResults blanks the payload of every tool result except the most
// recent keepRecent, and reports how many it cleared and how many characters
// that reclaimed.
//
// The messages are REWRITTEN, never removed. A tool result carries the
// ToolCallID pairing it with the assistant turn that asked for it, and both
// Anthropic and the OpenAI-compatible endpoints reject a tool call with no
// matching result — dropping the message would produce a 400 rather than a
// smaller request. (Contrast Budget.findRemovableTool, which does delete whole
// tool messages.)
//
// keepRecent <= 0 clears every tool result, which is a legitimate ask from a
// caller that has already decided none of them are load-bearing.
func ClearToolResults(messages []domain.Message, keepRecent int) (out []domain.Message, cleared, charsFreed int) {
	// Which tool results are recent enough to keep, found by walking backwards
	// so "recent" counts tool RESULTS rather than messages — a run whose last
	// twenty messages hold two tool calls should keep two, not none.
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
		// An already-cleared result must not be counted twice: this runs on
		// every turn, and a second pass over the same message would report
		// savings it did not make.
		if out[i].Content == note && len(out[i].Images) == 0 {
			continue
		}
		charsFreed += len(out[i].Content) - len(note)
		for _, img := range out[i].Images {
			charsFreed += len(img.Data)
		}
		out[i].Content = note
		// Images are the expensive half — a screenshot is worth hundreds of
		// tokens — and a picture of a screen from thirty turns ago is the least
		// likely thing in the transcript to still matter.
		out[i].Images = nil
		cleared++
	}
	return out, cleared, charsFreed
}
