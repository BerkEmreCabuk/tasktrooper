package antigravity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxStreamLine = 8 << 20

// event is one line of AGY's `--output-format stream-json`.
type event struct {
	Event          string         `json:"event"`
	ConversationID string         `json:"conversation_id"`
	Init           *initPayload   `json:"init,omitempty"`
	StepUpdate     *stepUpdate    `json:"step_update,omitempty"`
	Result         *resultPayload `json:"result,omitempty"`
}

type initPayload struct {
	Cwd   string   `json:"cwd"`
	Tools []string `json:"tools"`
}

type stepUpdate struct {
	StepIndex int              `json:"step_index"`
	State     string           `json:"state"`
	StepType  string           `json:"step_type"`
	TextDelta string           `json:"text_delta"`
	ToolName  string           `json:"tool_name"`
	ToolInfo  *toolInfoPayload `json:"tool_info"`
	Usage     *cliUsage        `json:"usage"`
}

type toolInfoPayload struct {
	Name       string          `json:"name"`
	Parameters json.RawMessage `json:"parameters"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type resultPayload struct {
	Status   string    `json:"status"`
	Response string    `json:"response"`
	NumTurns int       `json:"num_turns"`
	Usage    *cliUsage `json:"usage"`
}

type cliUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
}

func (u *cliUsage) toDomain() domain.Usage {
	if u == nil {
		return domain.Usage{}
	}
	return domain.Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
	}
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

func (s *streamingSink) OnTurn() { s.inner.OnTurn() }

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

type sessionInit struct {
	SessionID string
	Tools     []string
}

type initReporter interface {
	OnInit(init sessionInit)
}

func reportInit(s sink, init sessionInit) {
	if r, ok := s.(initReporter); ok {
		r.OnInit(init)
	}
}

type outcome struct {
	SessionID    string
	Init         sessionInit
	Text         string
	Usage        domain.Usage
	CostUSD      float64
	NumTurns     int
	Status       string
	IsError      bool
	ToolCalls    int
	ToolFailures int
	SawResult    bool
}

func parseStream(r io.Reader, s sink) (outcome, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	var out outcome
	lastAssistantText := ""
	lastTurnIndex := -1

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		if ev.ConversationID != "" {
			out.SessionID = ev.ConversationID
		}

		switch ev.Event {
		case "init":
			if ev.Init != nil {
				out.Init = sessionInit{
					SessionID: ev.ConversationID,
					Tools:     ev.Init.Tools,
				}
				s.OnSession(ev.ConversationID, "antigravity")
				reportInit(s, out.Init)
			}
		case "step_update":
			if ev.StepUpdate != nil {
				upd := ev.StepUpdate

				if upd.StepType == "agent_response" && upd.StepIndex != lastTurnIndex {
					s.OnTurn()
					lastTurnIndex = upd.StepIndex
				}

				if upd.StepType == "agent_response" && upd.TextDelta != "" {
					lastAssistantText += upd.TextDelta
					s.OnAssistantText(upd.TextDelta)
				} else if upd.StepType == "tool" {
					if upd.State == "ACTIVE" && upd.ToolInfo != nil && upd.ToolInfo.Parameters != nil {
						callID := fmt.Sprintf("call_%d", upd.StepIndex)
						s.OnToolUse(callID, upd.ToolName, string(upd.ToolInfo.Parameters))
					} else if (upd.State == "DONE" || upd.State == "ERROR") && upd.ToolInfo != nil {
						callID := fmt.Sprintf("call_%d", upd.StepIndex)
						out.ToolCalls++
						isErr := false
						errText := ""
						if upd.State == "ERROR" && upd.ToolInfo.Error != nil {
							isErr = true
							out.ToolFailures++
							errText = upd.ToolInfo.Error.Message
						}
						s.OnToolResult(callID, upd.ToolName, errText, isErr)
					}
				}
			}
		case "result":
			if ev.Result != nil {
				out.SawResult = true
				out.Status = ev.Result.Status
				out.IsError = ev.Result.Status != "SUCCESS"
				out.NumTurns = ev.Result.NumTurns
				out.Usage = ev.Result.Usage.toDomain()
				out.Text = firstNonEmpty(ev.Result.Response, lastAssistantText)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("read agy stream: %w", err)
	}
	if out.Text == "" {
		out.Text = lastAssistantText
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
