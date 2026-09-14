package llm

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// toolCallIDLength is what Mistral's request validator accepts: exactly nine
// alphanumeric characters. Hex digits satisfy it.
const toolCallIDLength = 9

// parseToolCalls converts a provider's tool calls and gives an id to any call
// that arrived without one.
//
// Mistral (and every OpenAI-compatible server that validates like it) matches
// each tool result to the assistant tool call it answers by id. A provider that
// returns `"id": null` therefore poisons the NEXT request instead of this one:
// the result message is built with an empty tool_call_id, `omitempty` drops the
// field from the wire, and the server rejects the whole conversation with
// `400 Unexpected tool call id None in tool message` — deterministically, so
// every retry fails too and the run dies. Synthesising the id here keeps the
// pairing intact for the rest of the run.
func parseToolCalls(calls []toolCall) []domain.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]domain.ToolCall, 0, len(calls))
	for i, tc := range calls {
		id := tc.ID
		if id == "" {
			id = synthToolCallID(tc.Function.Name, i)
		}
		callType := tc.Type
		if callType == "" {
			callType = "function"
		}
		out = append(out, domain.ToolCall{
			ID:   id,
			Type: callType,
			Function: domain.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return out
}

// synthToolCallID derives a stable nine-character id from the call's position
// in its turn, so the same response always yields the same id (a request the
// caller retries must not renumber calls the history already references).
func synthToolCallID(name string, index int) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name + "#" + strconv.Itoa(index)))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:toolCallIDLength]
}

// buildChatMessages renders the conversation for an OpenAI-compatible endpoint,
// dropping anything that would break the assistant/tool pairing rule.
func buildChatMessages(messages []domain.Message) []chatMessage {
	paired := normalizeToolPairing(messages)
	msgs := make([]chatMessage, 0, len(paired))
	// Images produced by the tool results of the batch currently being written.
	// They are flushed as a user turn once the batch ends — see flushToolImages.
	var pending []domain.ToolResultImage
	flush := func() {
		if len(pending) == 0 {
			return
		}
		msgs = append(msgs, chatMessage{
			Role:         string(domain.RoleUser),
			ContentParts: userImageContentParts(toolImagePreamble(len(pending)), pending),
		})
		pending = nil
	}
	for _, m := range paired {
		if m.Role != domain.RoleTool {
			flush()
		}
		cm := chatMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		// OpenAI-compatible tool messages are text-only, so the screenshot
		// cannot ride along with its own tool result. It is carried over to a
		// user turn right after the batch instead of being thrown away: a
		// screenshot the model never receives makes every "the page renders
		// correctly" verdict an unchecked claim, which is exactly how a broken
		// store badge passed both the developer and QA.
		if m.Role == domain.RoleTool && len(m.Images) > 0 {
			cm.Content += carriedImagesNote(len(m.Images))
			pending = append(pending, m.Images...)
		}
		// A user turn that carries images becomes a multimodal content array.
		// The user role is the one place this wire format allows it — the tool
		// role above genuinely cannot, which is why that one still degrades.
		if m.Role == domain.RoleUser && len(m.Images) > 0 {
			cm.ContentParts = userImageContentParts(m.Content, m.Images)
		}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, toolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: functionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		msgs = append(msgs, cm)
	}
	flush()
	return msgs
}

// carriedImagesNote tells the model, inside the tool result itself, where its
// screenshot went. Without it the result reads as text-only and the model asks
// the tool again for a picture that is already one message below.
func carriedImagesNote(n int) string {
	return fmt.Sprintf("\n[%d screenshot(s) attached — they are in the message right after this tool batch]", n)
}

// toolImagePreamble labels the carried user turn so the images are not mistaken
// for something a human just sent, and states the one rule that makes them
// worth carrying: a model that cannot actually see them must say so rather than
// describe what it assumes is there.
func toolImagePreamble(n int) string {
	return fmt.Sprintf("Here %s the %d screenshot(s) your last tool call captured. Look at %s and judge what is actually rendered — "+
		"broken images, missing assets, overlapping or clipped text, a control that is not where it should be. "+
		"If you cannot see images at all, say exactly that and do not give a visual verdict: an invented description of a "+
		"screenshot you never received is worse than no screenshot.",
		plural(n, "is", "are"), n, plural(n, "it", "them"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// userImageContentParts renders a user turn as the OpenAI-compatible
// multimodal content array: the text (when there is any) followed by one
// image_url part per attachment, each a base64 data: URI.
func userImageContentParts(text string, images []domain.ToolResultImage) []chatContentPart {
	parts := make([]chatContentPart, 0, len(images)+1)
	if text != "" {
		parts = append(parts, chatContentPart{Type: "text", Text: text})
	}
	for _, img := range images {
		parts = append(parts, chatContentPart{
			Type:     "image_url",
			ImageURL: &chatImageURL{URL: "data:" + img.MediaType + ";base64," + img.Data},
		})
	}
	return parts
}

// normalizeToolPairing enforces the invariant the wire format requires: every
// tool message answers a tool call in the assistant message right before it,
// and every assistant tool call is answered.
//
// The agent loop produces valid pairs on its own, but the history handed to it
// does not always survive intact — the token budget trims individual messages,
// and a persisted session replays assistant tool calls whose results were never
// stored. Either leaves an orphan, and an orphan is a 400 that kills the run
// rather than a degraded answer. Dropping the unpairable message costs one tool
// result; keeping it costs the conversation.
// systemReminder wraps a mid-conversation instruction for providers whose API
// has no system role inside the message array.
//
// The alternative — hoisting a late system message into the top-level system
// field — moves it from where it happened to the front of the prompt. That is
// wrong twice over: the instruction loses the position that gave it meaning
// ("you have 2 turns left" read as a standing rule), and rewriting the prompt's
// first bytes invalidates the entire cached prefix, so a single budget warning
// makes the rest of the run pay full price for every turn.
//
// The wrapper is what keeps the text from reading as something the user typed.
func systemReminder(content string) string {
	return "<system-reminder>\n" + content + "\n</system-reminder>"
}

func normalizeToolPairing(in []domain.Message) []domain.Message {
	out := make([]domain.Message, 0, len(in))
	for i := 0; i < len(in); i++ {
		m := in[i]

		if m.Role == domain.RoleTool {
			// A tool message only reaches here when no assistant tool call
			// preceded it — the branch below consumes the legitimate ones.
			continue
		}

		// An assistant turn with neither text nor tool calls is not a message the
		// wire format has a shape for: OpenAI-compatible servers reject the whole
		// request with `400 Assistant message must have either content or
		// tool_calls, but not none`. The model produces such a turn on its own
		// (an empty completion ends the agent loop), the session store persists
		// it, and from then on every later turn of that conversation replays it
		// and fails — deterministically, so the chat is dead until the row is
		// gone. Dropping it here costs nothing: there was nothing in it.
		if m.Role == domain.RoleAssistant && len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) == "" && len(m.Images) == 0 {
			continue
		}

		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			out = append(out, m)
			continue
		}

		answered := make(map[string]bool)
		for j := i + 1; j < len(in) && in[j].Role == domain.RoleTool; j++ {
			if in[j].ToolCallID != "" {
				answered[in[j].ToolCallID] = true
			}
		}

		kept := make([]domain.ToolCall, 0, len(m.ToolCalls))
		keptIDs := make(map[string]bool, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if tc.ID == "" || !answered[tc.ID] || keptIDs[tc.ID] {
				continue
			}
			kept = append(kept, tc)
			keptIDs[tc.ID] = true
		}

		if len(kept) > 0 {
			m.ToolCalls = kept
			out = append(out, m)
		} else if strings.TrimSpace(m.Content) != "" {
			m.ToolCalls = nil
			out = append(out, m)
		}

		seen := make(map[string]bool, len(kept))
		for i+1 < len(in) && in[i+1].Role == domain.RoleTool {
			i++
			result := in[i]
			if keptIDs[result.ToolCallID] && !seen[result.ToolCallID] {
				seen[result.ToolCallID] = true
				out = append(out, result)
			}
		}
	}
	return out
}
