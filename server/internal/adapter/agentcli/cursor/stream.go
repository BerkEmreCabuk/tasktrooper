package cursor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxStreamLine = 8 << 20

// event is one line of `cursor-agent -p --output-format stream-json`'s event
// stream (https://cursor.com/docs/cli/reference/output-format): system/init,
// assistant, tool_call (started/completed), result.
type event struct {
	Type      string                     `json:"type"`
	Subtype   string                     `json:"subtype"`
	SessionID string                     `json:"session_id"`
	Model     string                     `json:"model"`
	Message   *assistantMessage          `json:"message"`
	CallID    string                     `json:"call_id"`
	ToolCall  map[string]json.RawMessage `json:"tool_call"`
	IsError   bool                       `json:"is_error"`
	Result    string                     `json:"result"`
}

type assistantMessage struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// toolCallDetail is the value under the single dynamically-named key inside
// tool_call (e.g. "readToolCall"), whose name IS the tool with a "ToolCall"
// suffix.
type toolCallDetail struct {
	Args   json.RawMessage `json:"args"`
	Result *struct {
		Success *struct {
			Content string `json:"content"`
		} `json:"success"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"result"`
}

type sink interface {
	OnSession(sessionID, model string)
	OnTurn()
	OnAssistantText(text string)
	OnToolUse(callID, name, arguments string)
	OnToolResult(callID, name, content string, isError bool)
}

type streamingSink struct {
	inner    sink
	out      port.ChatStream
	streamed bool
}

func newStreamingSink(inner sink, out port.ChatStream) sink {
	if out.OnText == nil && out.OnSegmentBreak == nil {
		return inner
	}
	return &streamingSink{inner: inner, out: out}
}

func (s *streamingSink) OnSession(sessionID, model string) { s.inner.OnSession(sessionID, model) }
func (s *streamingSink) OnTurn()                           { s.inner.OnTurn() }

func (s *streamingSink) OnAssistantText(text string) {
	s.inner.OnAssistantText(text)
	if text != "" {
		s.out.Text(text)
		s.streamed = true
	}
}

func (s *streamingSink) OnToolUse(callID, name, arguments string) {
	s.inner.OnToolUse(callID, name, arguments)
	if s.streamed {
		s.out.SegmentBreak()
		s.streamed = false
	}
}

func (s *streamingSink) OnToolResult(callID, name, content string, isError bool) {
	s.inner.OnToolResult(callID, name, content, isError)
}

// outcome is everything one cursor-agent session produced.
//
// It carries no usage/token fields: the documented stream-json schema names
// none for cursor-agent the way claudecode's and opencode's schemas do, so a
// run's cost never reaches usageapp for this provider.
type outcome struct {
	SessionID    string
	Model        string
	Text         string
	IsError      bool
	Status       string
	ToolCalls    int
	ToolFailures int
	SawResult    bool
}

// parseStream reads one cursor-agent session's stream-json event stream.
//
// Unlike claudecode's text_delta or opencode's growing-snapshot part, an
// "assistant" event here carries the COMPLETE message text in one shot, so
// there is no accumulation to do — see the Cursor CLI output-format
// reference.
func parseStream(r io.Reader, s sink) (outcome, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	var out outcome
	turnSeen := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.SessionID != "" {
			out.SessionID = ev.SessionID
		}
		switch ev.Type {
		case "system":
			if ev.Subtype == "init" {
				out.Model = ev.Model
				s.OnSession(ev.SessionID, ev.Model)
			}
		case "assistant":
			if !turnSeen {
				s.OnTurn()
				turnSeen = true
			}
			if ev.Message != nil {
				for _, c := range ev.Message.Content {
					if c.Type == "text" && c.Text != "" {
						out.Text = c.Text
						s.OnAssistantText(c.Text)
					}
				}
			}
		case "tool_call":
			name, detail := firstToolCall(ev.ToolCall)
			switch ev.Subtype {
			case "started":
				s.OnToolUse(ev.CallID, name, string(detail.Args))
			case "completed":
				out.ToolCalls++
				isErr := detail.Result != nil && detail.Result.Error != nil
				content := ""
				if detail.Result != nil {
					if detail.Result.Success != nil {
						content = detail.Result.Success.Content
					} else if detail.Result.Error != nil {
						content = detail.Result.Error.Message
					}
				}
				if isErr {
					out.ToolFailures++
				}
				s.OnToolResult(ev.CallID, name, content, isErr)
			}
		case "result":
			out.SawResult = true
			out.IsError = ev.IsError
			out.Status = ev.Subtype
			if strings.TrimSpace(ev.Result) != "" {
				out.Text = ev.Result
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("read cursor-agent stream: %w", err)
	}
	return out, nil
}

// firstToolCall pulls the one entry cursor-agent's dynamically-keyed
// tool_call wrapper carries, and reports the tool's name with the ToolCall
// suffix stripped ("readToolCall" -> "read").
func firstToolCall(m map[string]json.RawMessage) (string, toolCallDetail) {
	for k, raw := range m {
		var d toolCallDetail
		_ = json.Unmarshal(raw, &d)
		return strings.TrimSuffix(k, "ToolCall"), d
	}
	return "", toolCallDetail{}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
