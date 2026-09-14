package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// maxStreamLine bounds one line of the CLI's stream. The events are single-line
// JSON objects and one of them carries a whole tool result — a file read, a
// build log — so the 64KB bufio.Scanner default is far too small: it would stop
// the scan mid-run with "token too long" and lose everything after it. 8MB is
// past any plausible single event and still a hard ceiling, because the reader
// is a subprocess this process does not control.
const maxStreamLine = 8 << 20

// event is one line of `--output-format stream-json`.
//
// Only the fields this executor acts on are declared. The CLI adds fields
// between versions and json.Unmarshal ignores unknown ones, which is the
// property that keeps a `claude` upgrade from breaking the parse: a stream
// whose events grew a field still parses, and a stream whose events LOST one
// this reads degrades to a zero value rather than an error.
type event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	// SessionID is on nearly every event; the init one is where it first
	// appears, and it is what a parked run resumes with.
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	// Tools and MCPServers are the init event's inventory: what the session was
	// actually given. They are read because the answer to "why did this run
	// behave as though the board did not exist" is in them, and was until now
	// recoverable only from the agent's own prose.
	//
	// Tools holds the CLI's NATIVE tools. It does not list MCP tools while the
	// servers are still connecting, and past a tool-count threshold the CLI
	// stops sending schemas up front at all and puts them behind ToolSearch —
	// so an empty MCP share here is not evidence of a missing endpoint. The
	// server list is.
	Tools []string `json:"tools"`
	// A POINTER because absent and empty mean opposite things: a CLI build that
	// does not report its servers tells us nothing, while one that reports an
	// empty list is saying it loaded none — and only the second is a fault.
	MCPServers *[]mcpServerState `json:"mcp_servers"`
	// Message carries the assistant/user turn. Absent on system and result
	// events.
	Message *cliMessage `json:"message"`
	// Result and the fields below it are the terminal event's own.
	Result       string    `json:"result"`
	IsError      bool      `json:"is_error"`
	NumTurns     int       `json:"num_turns"`
	TotalCostUSD float64   `json:"total_cost_usd"`
	Usage        *cliUsage `json:"usage"`
	// Error is what some builds put the failure text in instead of result.
	Error string `json:"error"`
}

// mcpServerState is one entry of the init event's server list: every MCP server
// the CLI resolved from its configuration, and how that connection went.
//
// Status is the CLI's own word — "connected", "pending" while the handshake is
// in flight, "needs-auth", "failed". Only absence from the list is read as a
// fault here (see initGuard): a server named in --mcp-config that the CLI does
// not list was never loaded at all, while a "pending" one is usually just the
// handshake outrunning the init event.
type mcpServerState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// sessionInit is what the init event said about the session's surface.
type sessionInit struct {
	SessionID string
	Model     string
	Tools     []string
	Servers   []mcpServerState
	// ServersReported says the CLI listed its MCP servers at all. False means
	// the field was absent — an older build, or a stub in a test — and nothing
	// about the endpoint may be concluded from the empty Servers beside it.
	ServersReported bool
}

// server returns the state of the named MCP server, and whether the CLI
// mentioned it at all.
func (i sessionInit) server(name string) (mcpServerState, bool) {
	for _, s := range i.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return mcpServerState{}, false
}

// serverNames lists every MCP server the session loaded, for the log line that
// says what a run was actually holding.
func (i sessionInit) serverNames() []string {
	names := make([]string, 0, len(i.Servers))
	for _, s := range i.Servers {
		names = append(names, s.Name+"="+s.Status)
	}
	return names
}

// initReporter is the optional half of sink: a sink that wants the init
// event's inventory implements it, and one that does not is unaffected.
//
// Optional rather than a method on sink because only ONE sink cares — the guard
// spawn wraps around the trace to fail a session that started without the tools
// its run needs — and widening the interface would have every implementation
// and every test fake grow a method they ignore.
type initReporter interface {
	OnInit(init sessionInit)
}

// reportInit hands the inventory to s when it wants it.
func reportInit(s sink, init sessionInit) {
	if r, ok := s.(initReporter); ok {
		r.OnInit(init)
	}
}

type cliMessage struct {
	Role string `json:"role"`
	// Content is an array of blocks on an assistant turn, but the CLI also
	// emits a plain string for simple user turns. RawMessage + a two-shot
	// decode is the only way to accept both without failing the line.
	Content json.RawMessage `json:"content"`
	Usage   *cliUsage       `json:"usage"`
}

type contentBlock struct {
	Type string `json:"type"`
	// text block
	Text string `json:"text"`
	// tool_use block
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result block
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// cliUsage is Anthropic's native accounting, which splits the cached share OUT
// of input_tokens. domain.Usage's contract is the opposite — PromptTokens is
// the TOTAL with the cache figures as subsets — so toDomain adds them back,
// exactly as the anthropic HTTP adapter does. Getting this backwards would
// under-report every cached run's prompt size and under-bill it.
type cliUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u *cliUsage) toDomain() domain.Usage {
	if u == nil {
		return domain.Usage{}
	}
	prompt := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return domain.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

// sink receives the stream's events as they arrive rather than at the end.
//
// Live, because the run's trace is what a human watches while a task is being
// worked: collecting the whole stream and writing it at exit would leave the
// activity feed blank for the length of a task and then fill it in at once,
// which is exactly the "is it doing anything?" the feed exists to answer.
type sink interface {
	// OnSession fires once, on the init event, with the id a park would resume.
	OnSession(sessionID, model string)
	// OnTurn fires once per assistant turn, BEFORE that turn's blocks are
	// reported, and only for a turn that has something in it.
	//
	// It exists because the trace needs the same shape the agent loop's has. A
	// loop run brackets each LLM round in an iteration_start, and everything that
	// reads a trace — the activity graph in the SPA above all — hangs the round's
	// narration and tool calls off that bracket. A CLI session emitted the
	// contents with no bracket at all, so the reader had a flat pile of steps
	// belonging to no round, and the SPA, which looks up the enclosing round
	// before attaching a tool call, dropped every one of them on the floor.
	//
	// An assistant event IS the CLI's round: one model turn, its text and the
	// tools it asked for. Nothing is invented by marking it.
	OnTurn()
	// OnAssistantText fires for every text block the assistant produced.
	OnAssistantText(text string)
	// OnToolUse fires when the assistant asks for a tool. arguments is the raw
	// JSON input, passed through rather than re-encoded.
	OnToolUse(callID, name, arguments string)
	// OnToolResult fires when that call's result comes back. It is the only
	// place the tool ledger may be written from: a tool_use is a REQUEST, and
	// counting requests would credit a run for work that never ran.
	OnToolResult(callID, name, content string, isError bool)
}

// streamingSink forwards the assistant's text to a live listener on its way
// through, and otherwise delegates everything to the trace sink underneath.
//
// It is a decorator rather than a flag on traceSink because the two answer to
// different masters: traceSink writes the run's permanent record (activity
// steps, the tool ledger the grounding gates read), while this one feeds a
// browser that is watching right now. A board run gets the record only, and
// newStreamingSink hands back the bare traceSink for it, so nothing about the
// board path changes.
//
// # Why the segment break matters here
//
// A CLI session narrates between tool calls — "let me look at the router
// first" — and its terminal result event carries ONLY the closing answer. Stream
// every text block with no boundary and the browser shows the whole narration
// as the reply, then swaps it for the shorter text that gets persisted; the user
// watches their answer get shorter. The break says "everything up to here was
// thinking", which is the same contract the agent loop's own streamed turns
// already have with the SSE handler, so the client needs no new behaviour.
type streamingSink struct {
	inner sink
	out   port.ChatStream
	// streamed records whether any text has gone out since the last boundary,
	// so a session that opens with a tool call does not emit a break over
	// nothing.
	streamed bool
}

// newStreamingSink wraps inner only when someone is actually listening.
func newStreamingSink(inner sink, out port.ChatStream) sink {
	if out.OnText == nil && out.OnSegmentBreak == nil {
		return inner
	}
	return &streamingSink{inner: inner, out: out}
}

func (s *streamingSink) OnSession(sessionID, model string) { s.inner.OnSession(sessionID, model) }

// OnTurn is a pure record-keeping event: nothing about it belongs on the wire to
// a browser watching a chat reply, so it passes straight through.
func (s *streamingSink) OnTurn() { s.inner.OnTurn() }

func (s *streamingSink) OnAssistantText(text string) {
	s.inner.OnAssistantText(text)
	// The CLI emits whole text blocks rather than tokens, and consecutive blocks
	// within a turn are separate paragraphs. Without the separator they arrive
	// welded together in the transcript.
	if s.streamed {
		s.out.Text("\n\n")
	}
	s.out.Text(text)
	s.streamed = true
}

func (s *streamingSink) OnToolUse(callID, name, arguments string) {
	s.inner.OnToolUse(callID, name, arguments)
	// The text before this call was the reasoning that led to it. Closing the
	// segment now is what keeps it from being read as the answer.
	if s.streamed {
		s.out.SegmentBreak()
		s.streamed = false
	}
}

func (s *streamingSink) OnToolResult(callID, name, content string, isError bool) {
	s.inner.OnToolResult(callID, name, content, isError)
}

// outcome is what the terminal result event said.
type outcome struct {
	SessionID string
	// Init is what the session STARTED with: its tools and its MCP servers. Kept
	// on the outcome so a finished run can be explained from one value.
	Init sessionInit
	// Text is the run's closing answer: the result event's own text when it has
	// one, else the last assistant text block seen.
	Text         string
	Usage        domain.Usage
	CostUSD      float64
	NumTurns     int
	Subtype      string
	IsError      bool
	ToolCalls    int
	ToolFailures int
	// Sawresult records whether a terminal event arrived at all. A stream that
	// ends without one is a killed process, not a finished run, and must not be
	// reported as one.
	SawResult bool
}

// parseStream reads the CLI's stream-json output to the end, reporting events
// to s as they arrive and returning what the terminal event said.
//
// A malformed line is skipped rather than fatal: the stream is a subprocess's
// stdout and can legitimately carry a stray non-JSON line (a warning from a
// wrapper script, a progress line from a tool). Failing the whole run on one
// would turn a cosmetic mismatch into a lost task; the missing terminal event
// is what the caller checks instead.
func parseStream(r io.Reader, s sink) (outcome, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	var out outcome
	lastAssistantText := ""
	// toolNames maps a tool_use id to its name so the RESULT event, which
	// carries only the id, can be attributed to the tool that produced it.
	toolNames := map[string]string{}

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
				out.Init = sessionInit{
					SessionID:       ev.SessionID,
					Model:           ev.Model,
					Tools:           ev.Tools,
					ServersReported: ev.MCPServers != nil,
				}
				if ev.MCPServers != nil {
					out.Init.Servers = *ev.MCPServers
				}
				s.OnSession(ev.SessionID, ev.Model)
				reportInit(s, out.Init)
			}
		case "assistant":
			blocks := decodeBlocks(ev.Message)
			// The turn is opened only when it has something in it. An assistant
			// event carrying nothing this executor reports (thinking blocks alone,
			// a whitespace-only text block) would otherwise open an empty round in
			// the trace — a node the reader has to expand to discover it is blank.
			if turnHasContent(blocks) {
				s.OnTurn()
			}
			for _, block := range blocks {
				switch block.Type {
				case "text":
					if strings.TrimSpace(block.Text) == "" {
						continue
					}
					lastAssistantText = block.Text
					s.OnAssistantText(block.Text)
				case "tool_use":
					toolNames[block.ID] = block.Name
					s.OnToolUse(block.ID, block.Name, string(block.Input))
				}
			}
		case "user":
			// Tool results come back as a user turn — that is how the Anthropic
			// message format carries them, and the CLI echoes the format.
			for _, block := range decodeBlocks(ev.Message) {
				if block.Type != "tool_result" {
					continue
				}
				name := toolNames[block.ToolUseID]
				out.ToolCalls++
				if block.IsError {
					out.ToolFailures++
				}
				s.OnToolResult(block.ToolUseID, name, blockText(block.Content), block.IsError)
			}
		case "result":
			out.SawResult = true
			out.Subtype = ev.Subtype
			out.IsError = ev.IsError
			out.NumTurns = ev.NumTurns
			out.CostUSD = ev.TotalCostUSD
			out.Usage = ev.Usage.toDomain()
			out.Text = firstNonEmpty(ev.Result, ev.Error, lastAssistantText)
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("read claude code stream: %w", err)
	}
	if out.Text == "" {
		out.Text = lastAssistantText
	}
	return out, nil
}

// turnHasContent reports whether an assistant turn holds anything parseStream
// will report — the exact test the loop below applies block by block, so a turn
// is opened if and only if at least one step follows it.
func turnHasContent(blocks []contentBlock) bool {
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				return true
			}
		case "tool_use":
			return true
		}
	}
	return false
}

// decodeBlocks accepts both shapes the content field takes: the array of blocks
// an assistant turn carries, and the bare string a simple user turn does. A
// string has no blocks worth reporting, so it decodes to nothing rather than to
// an error.
func decodeBlocks(msg *cliMessage) []contentBlock {
	if msg == nil || len(msg.Content) == 0 {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		return blocks
	}
	return nil
}

// blockText renders a tool_result's content, which is a string on simple
// results and an array of blocks on rich ones. Only the text is kept: the
// activity trace shows a preview, and an image inside a CLI tool result is the
// CLI's business, not this transcript's.
func blockText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
