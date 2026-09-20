package antigravity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxStreamLine = 8 << 20

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

type sink = core.Sink

type outcome = core.Outcome

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
				init := sessionInit{
					SessionID: ev.ConversationID,
					Tools:     ev.Init.Tools,
				}
				s.OnSession(ev.ConversationID, "antigravity")
				reportInit(s, init)
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
				out.Text = core.FirstNonEmpty(ev.Result.Response, lastAssistantText)
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