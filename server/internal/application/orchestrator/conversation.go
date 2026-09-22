package orchestrator

import (
	"errors"
	"fmt"
	"strings"

	goccyjson "github.com/goccy/go-json"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// System messages dropped for the toolless stages — except the action ledger, the only record of opened board tasks.
func pipelineConversationHistory(history []domain.Message) []domain.Message {
	out := make([]domain.Message, 0, len(history))
	for _, m := range history {
		switch m.Role {
		case domain.RoleSystem:
			if domain.IsSessionActionDigest(m.Content) {
				out = append(out, domain.Message{Role: m.Role, Content: m.Content})
			}
		case domain.RoleUser:
			if strings.TrimSpace(m.Content) != "" {
				out = append(out, domain.Message{Role: m.Role, Content: m.Content})
			}
		case domain.RoleAssistant:
			if len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != "" {
				out = append(out, domain.Message{Role: m.Role, Content: m.Content})
			}
		}
	}
	return out
}

func buildPipelineLLMMessages(systemPrompt string, history []domain.Message, userMessage string, extraContext []domain.Message, corrections []domain.Message) []domain.Message {
	conversation := pipelineConversationHistory(history)
	capacity := 1 + len(extraContext) + len(conversation) + 1 + len(corrections)
	messages := make([]domain.Message, 0, capacity)
	messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: systemPrompt})
	for _, m := range extraContext {
		if m.Role == domain.RoleSystem {
			messages = append(messages, m)
		}
	}
	messages = append(messages, conversation...)
	messages = appendPipelineUserMessage(messages, userMessage)
	messages = append(messages, corrections...)
	return messages
}

// Strips json fences and narrows to the outermost {...} so preamble/trailing prose does not break the parse.
func stripJSONWrapper(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			end := len(lines) - 1
			for end > 0 && strings.TrimSpace(lines[end]) != "```" {
				end--
			}
			if end > 0 {
				trimmed = strings.Join(lines[1:end], "\n")
			}
		}
	}
	trimmed = strings.TrimSpace(trimmed)
	return extractFirstJSONObject(trimmed)
}

// Retries once with stray commas repaired; the strict parse's original error survives the repair.
func parseLLMJSON(content string, v any) error {
	err := goccyjson.Unmarshal([]byte(content), v)
	if err == nil {
		return nil
	}
	repaired := repairJSONCommas(content)
	if repaired == content {
		return err
	}
	if repairErr := goccyjson.Unmarshal([]byte(repaired), v); repairErr != nil {
		return err
	}
	log.Warn().Err(err).Msg("model json had stray commas; parsed after repair")
	return nil
}

// Removes the commas that cannot legally be there; string literals are copied untouched.
func repairJSONCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	esc := false
	// last meaningful character outside a string; whitespace does not count.
	var prev byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
				prev = c
			}
			continue
		}
		if c == ',' {
			if prev == '{' || prev == '[' || prev == ',' || prev == 0 {
				continue
			}
			if next := nextNonSpace(s, i+1); next == '}' || next == ']' {
				continue
			}
		}
		b.WriteByte(c)
		if c == '"' {
			inStr = true
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			prev = c
		}
	}
	return b.String()
}

func nextNonSpace(s string, i int) byte {
	for ; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return s[i]
		}
	}
	return 0
}

// First balanced {...} object, stopping at the matching close so trailing junk is dropped.
func extractFirstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return s
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

// Feeds the parse failure back with the offending spot quoted, not just the raw error.
func pipelineCorrection(badResponse string, err error) []domain.Message {
	instruction := "Your response could not be parsed: " + err.Error() + "."
	if where := jsonErrorContext(badResponse, err); where != "" {
		instruction += "\n" + where
	}
	instruction += "\nFix that exact spot. Common causes: a comma before a closing } or ], two commas in a row, an empty array element like [,], a missing value, or an unescaped quote inside a string."
	instruction += "\nReturn ONLY corrected valid JSON matching the schema, with no other text."
	return append(correctionEcho(badResponse), domain.Message{Role: domain.RoleUser, Content: instruction})
}

// The bad answer is echoed so the retry has it in context — but an empty assistant message would 400.
func correctionEcho(badResponse string) []domain.Message {
	if strings.TrimSpace(badResponse) == "" {
		return []domain.Message{}
	}
	return []domain.Message{{Role: domain.RoleAssistant, Content: badResponse}}
}

const jsonErrorContextWindow = 80

// Quotes the document around the byte the parser stopped at; "" when the error carries no offset.
func jsonErrorContext(raw string, err error) string {
	doc := stripJSONWrapper(raw)
	offset := -1
	var syntaxErr *goccyjson.SyntaxError
	var typeErr *goccyjson.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr):
		offset = int(syntaxErr.Offset)
	case errors.As(err, &typeErr):
		offset = int(typeErr.Offset)
	}
	if offset <= 0 || offset > len(doc) {
		return ""
	}
	start := max(offset-jsonErrorContextWindow, 0)
	end := min(offset+jsonErrorContextWindow, len(doc))
	before := strings.ToValidUTF8(doc[start:offset], "")
	after := strings.ToValidUTF8(doc[offset:end], "")
	return fmt.Sprintf("It broke at character %d of your JSON, marked <<<HERE>>> below:\n%s<<<HERE>>>%s", offset, before, after)
}

// Feeds a rule violation back as a rejection, not a parse failure, so the retry can act on it.
func pipelineRejection(badResponse string, err error) []domain.Message {
	return append(correctionEcho(badResponse), domain.Message{
		Role:    domain.RoleUser,
		Content: "Your plan was rejected: " + err.Error() + ". Fix exactly that problem, keep the rest of the plan as it is, and return ONLY the corrected valid JSON matching the schema, with no other text.",
	})
}

func appendPipelineUserMessage(messages []domain.Message, userMessage string) []domain.Message {
	trimmed := strings.TrimSpace(userMessage)
	if trimmed == "" {
		return messages
	}
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		if last.Role == domain.RoleUser && last.Content == trimmed {
			return messages
		}
	}
	return append(messages, domain.Message{Role: domain.RoleUser, Content: trimmed})
}
