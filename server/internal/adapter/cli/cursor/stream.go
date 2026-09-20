package cursor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

const maxStreamLine = 8 << 20

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

type sink = core.Sink

type outcome = core.Outcome

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

func firstToolCall(m map[string]json.RawMessage) (string, toolCallDetail) {
	for k, raw := range m {
		var d toolCallDetail
		_ = json.Unmarshal(raw, &d)
		return strings.TrimSuffix(k, "ToolCall"), d
	}
	return "", toolCallDetail{}
}