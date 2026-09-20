package llm

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const toolCallIDLength = 9

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

func synthToolCallID(name string, index int) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name + "#" + strconv.Itoa(index)))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:toolCallIDLength]
}

func buildChatMessages(messages []domain.Message) []chatMessage {
	paired := normalizeToolPairing(messages)
	msgs := make([]chatMessage, 0, len(paired))

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

		if m.Role == domain.RoleTool && len(m.Images) > 0 {
			cm.Content += carriedImagesNote(len(m.Images))
			pending = append(pending, m.Images...)
		}

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

func carriedImagesNote(n int) string {
	return fmt.Sprintf("\n[%d screenshot(s) attached — they are in the message right after this tool batch]", n)
}

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

func systemReminder(content string) string {
	return "<system-reminder>\n" + content + "\n</system-reminder>"
}

func normalizeToolPairing(in []domain.Message) []domain.Message {
	out := make([]domain.Message, 0, len(in))
	for i := 0; i < len(in); i++ {
		m := in[i]

		if m.Role == domain.RoleTool {
			continue
		}

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
