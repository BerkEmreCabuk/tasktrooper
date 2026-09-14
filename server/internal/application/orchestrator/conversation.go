package orchestrator

import (
	"errors"
	"fmt"
	"strings"

	goccyjson "github.com/goccy/go-json"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// pipelineConversationHistory reduces the session history to what the toolless
// pipeline stages (intake, planner) can act on.
//
// System messages are dropped — they belong to the agent loop, not to planning —
// with one exception: the session action ledger. It is the only record of which
// board tasks this conversation already opened, and it is written as a system
// message. Dropping it meant intake and the planner planned every turn as if the
// board were empty, so "move the analiz task onto the board" was planned as
// "create a task" and produced a second record for work already there.
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

// buildPipelineLLMMessages assembles the message list for planner/intake calls.
// extraContext: system-role context messages injected before conversation history.
// corrections: assistant+user turn appended after the user message for self-correction retries.
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

// stripJSONWrapper pulls a JSON object out of LLM output that may be wrapped in
// ```json fences or padded with prose (e.g. a leading "Note:"/"No changes..."
// sentence — the classic "invalid character 'N'" parse failure). It strips code
// fences, then narrows to the outermost '{' … '}' so a stray preamble or trailing
// remark no longer breaks the first-attempt parse.
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

// parseLLMJSON unmarshals a JSON answer a model wrote, retrying once with the
// comma mistakes repaired.
//
// Models emit `[,]`, `[a,,b]` and a trailing `,}` often enough that the planner
// died on them: goccy rejects the document with "invalid character ',' looking
// for beginning of value", the stage retries with the error fed back, and a
// model that makes the mistake once tends to make it again — three attempts,
// then the whole run fails with no plan. None of those commas carry meaning, so
// removing them recovers the model's actual answer instead of discarding it.
//
// The strict parse runs first and its error is what the caller sees when the
// repair does not help: a document broken in some other way must still fail.
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

// repairJSONCommas removes the commas that cannot legally be where they are: one
// directly after an opening brace or bracket, one directly before a closing one,
// and any repeat of a comma. Everything inside a string literal is copied
// untouched, so a comma in a task description is never affected.
func repairJSONCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	esc := false
	// prev is the last meaningful character written outside a string; whitespace
	// does not count, so `[ , 1]` is repaired exactly like `[,1]`.
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

// nextNonSpace returns the next non-whitespace byte at or after i, or 0 at the
// end of the string.
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

// extractFirstJSONObject returns the first balanced {...} object in s. Unlike a
// first-brace/last-brace slice, it stops at the matching close brace, so any
// trailing junk a model appends after the JSON — a stray backtick, a closing
// ``` fence, or a second example block — is dropped instead of being fed to the
// parser (which otherwise fails with "invalid character '`' after top-level
// value"). String contents and escapes are respected so braces inside strings
// don't throw off the depth count.
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

// pipelineCorrection feeds a parse failure back so the model can fix its own
// output. The error alone was not enough to fix anything: "invalid character
// ',' looking for beginning of value" names neither the place nor the mistake,
// and a model that cannot see WHERE it went wrong returns the same document —
// three attempts, then the run dies. The offending spot is quoted now, with the
// position marked.
func pipelineCorrection(badResponse string, err error) []domain.Message {
	instruction := "Your response could not be parsed: " + err.Error() + "."
	if where := jsonErrorContext(badResponse, err); where != "" {
		instruction += "\n" + where
	}
	instruction += "\nFix that exact spot. Common causes: a comma before a closing } or ], two commas in a row, an empty array element like [,], a missing value, or an unescaped quote inside a string."
	instruction += "\nReturn ONLY corrected valid JSON matching the schema, with no other text."
	return append(correctionEcho(badResponse), domain.Message{Role: domain.RoleUser, Content: instruction})
}

// correctionEcho replays the model's own bad answer before the correction — it
// is the thing being corrected, so it has to be in the conversation.
//
// Unless it is empty: an assistant message with no content and no tool calls is
// rejected by OpenAI-compatible providers with a 400 that fails the whole
// request, so echoing an empty completion back would turn "the model said
// nothing" into "the retry cannot be sent at all".
func correctionEcho(badResponse string) []domain.Message {
	if strings.TrimSpace(badResponse) == "" {
		return []domain.Message{}
	}
	return []domain.Message{{Role: domain.RoleAssistant, Content: badResponse}}
}

// jsonErrorContextWindow is how much of the document is quoted around the
// failure — enough to see the line it is on, short enough to stay readable.
const jsonErrorContextWindow = 80

// jsonErrorContext quotes the document around the byte the parser stopped at,
// with a marker at the exact position. Returns "" when the error carries no
// offset (a missing-field error, say, which is about the schema and not a spot).
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

// pipelineRejection feeds a rule violation back to the model. The response
// parsed fine — it broke a plan rule — so telling it the JSON was unreadable
// would be a lie it cannot act on. Without this the retry re-sent the identical
// prompt and the model returned the identical rejected plan until the attempts
// ran out ("planner failed after 3 attempts: parallel_group 0 has 5 subtasks
// that write to the board"), so a fixable plan surfaced to the user as a hard
// failure.
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
