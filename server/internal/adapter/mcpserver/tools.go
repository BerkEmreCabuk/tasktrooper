package mcpserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ServedToolNames lists what this endpoint would serve run, by registry name
// and in the order tools/list returns them.
//
// Exported for the Claude Code executor, which puts the list into the session's
// system prompt so the model is told what it holds. It takes the whole Run
// rather than just its policy because the policy is no longer the only thing
// that narrows the surface — see Run.SkillsOnDisk — and a manifest computed
// from half the inputs would name a tool the endpoint then refuses.
func ServedToolNames(registry port.ToolRegistry, run Run) []string {
	tools := servedTools(registry, run)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// servedTools is THE surface one run gets. tools/list renders it, tools/call
// gates on it, and the session's prompt manifest is named off it, so every
// question of the form "does this run hold X" is answered in one place.
//
// That single answer is the whole design. Filtering the advertised list is not
// access control — a client may call any name it likes — so the check has to
// exist twice; deriving both from this function is what stops the two copies
// from drifting into a surface that advertises one set and executes another.
func servedTools(registry port.ToolRegistry, run Run) []toolInfo {
	if registry == nil {
		return nil
	}
	return toolsFrom(registry.DefinitionsForPolicy(run.Policy), run.SkillsOnDisk)
}

// nativelyCovered are the registry tools this endpoint does NOT hand to a
// Claude Code session, because the CLI already ships a better one.
//
// "Better" is not a matter of taste. The CLI's Read paginates and remembers
// what it has read; its Edit refuses a stale write it did not read first; its
// Bash keeps a live shell with its own timeout and output handling; its Grep and
// Glob are ripgrep. Every one of those is what the model was trained against.
// Serving TaskTrooper's equivalents alongside them would give the session two
// tools for the same job — the classic way to make a model pick the worse one —
// and would route file bytes through a JSON-RPC round trip for nothing.
//
// What is NOT here is as deliberate: codebase_search, get_symbol_skeleton and
// expand_symbol_context stay, because they are backed by the semantic index
// (application/indexer) and the CLI has no equivalent at all — grep finds a
// string, those find the code that means something.
var nativelyCovered = map[string]struct{}{
	"run_terminal":  {}, // Bash
	"read_file":     {}, // Read
	"write_file":    {}, // Write
	"edit_file":     {}, // Edit
	"edit_lines":    {}, // Edit
	"delete_file":   {}, // Bash
	"move_file":     {}, // Bash
	"grep_code":     {}, // Grep
	"get_repo_tree": {}, // Glob
}

// skillLoadTool hands the session one skill's body over MCP.
//
// It is deliberately NOT in nativelyCovered, because whether it is redundant is
// a property of the RUN rather than of the CLI. For a run whose skills arrived
// as an index in the system prompt it is the only way to read one at all;
// for a run whose skills application/agentfs wrote into the workspace it is a
// second, worse path to a file the session can already see. Only the second
// kind loses it — see Run.SkillsOnDisk.
//
// create_skill is not paired with it and must not be. Authoring a skill is
// independent of reading one: the CLI discovers skill files, it does not write
// them back into this agent's catalog, and prompt.SkillsOnDiskMessage still
// asks a self-evolving agent to save what this task forced it to work out.
// Dropping create_skill here would turn self-evolution off for every board CLI
// run while looking like a tidy symmetry.
const skillLoadTool = "load_skill"

// exposed reports whether one registry tool may be reached over MCP at all,
// before any policy filtering. skillsOnDisk is the run's — see Run.
//
// ask_user is refused here rather than left to the policy. It does not return a
// result: it hands a domain.ClarificationRequest back, which the agent loop
// turns into a parked task waiting for a human to answer in a chat thread. A
// live CLI session has no such pause — it would sit on an open JSON-RPC call
// while the run around it was being parked, and neither side could finish.
//
// Clarification for claude_code runs is therefore future work: it needs the
// session to be suspendable (the quota park already proves a session can be
// resumed by id, so the shape exists) before the question can be asked at all.
// Until then a claude_code agent that is missing information has to say so in
// its closing message, which the runner already surfaces on the card.
func exposed(name string, skillsOnDisk bool) bool {
	if name == domain.AskUserToolName {
		return false
	}
	if skillsOnDisk && name == skillLoadTool {
		return false
	}
	_, native := nativelyCovered[name]
	return !native
}

// toolInfo is one entry of an MCP tools/list result.
type toolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// emptyObjectSchema is what a tool with no parameters advertises. The MCP spec
// requires inputSchema to be an object schema, and a null one makes some
// clients drop the tool entirely rather than call it with no arguments.
func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// toolsFrom converts the registry's OpenAI-shaped definitions into MCP tools,
// dropping the ones this endpoint does not serve.
//
// Sorted by name so two calls with the same policy produce the same list: the
// registry stores tools in a map, and an order that changed per request would
// churn the CLI's own prompt for no reason.
func toolsFrom(defs []domain.ToolDefinition, skillsOnDisk bool) []toolInfo {
	tools := make([]toolInfo, 0, len(defs))
	for _, def := range defs {
		name := def.Function.Name
		if name == "" || !exposed(name, skillsOnDisk) {
			continue
		}
		schema := def.Function.Parameters
		if len(schema) == 0 {
			schema = emptyObjectSchema()
		}
		tools = append(tools, toolInfo{
			Name:        name,
			Description: def.Function.Description,
			InputSchema: schema,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

// content is one MCP content block. Only the two kinds a TaskTrooper tool can
// produce are modelled: text, and the inline screenshots the mobile_* and
// browser_* tools return (see adapter/tools/mobile/observe.go).
type content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// callToolResult is an MCP tools/call result.
type callToolResult struct {
	Content []content `json:"content"`
	// IsError is how a TOOL failure is reported — not a JSON-RPC error. The
	// distinction is what the client does with it: a JSON-RPC error is a
	// protocol fault the CLI reports to its operator and the model never sees,
	// while isError puts the failure text in front of the model, which is the
	// only place a wrong argument can actually be fixed.
	IsError bool `json:"isError,omitempty"`
}

func textResult(text string, isError bool) callToolResult {
	return callToolResult{Content: []content{{Type: "text", Text: text}}, IsError: isError}
}

// resultToMCP maps a domain.ToolResult onto MCP content.
func resultToMCP(name string, result domain.ToolResult) callToolResult {
	// A clarification cannot reach here — ask_user is not served (see exposed)
	// and it is the only tool that produces one — but a result carrying one
	// would otherwise be answered with empty content, which reads to the model
	// as a tool that silently did nothing. Say what happened instead.
	if result.Clarification != nil {
		return textResult(fmt.Sprintf(
			"%s asked the human a question, which a Claude Code session cannot wait for. Decide with what you have, or explain what is missing in your final message.",
			name), true)
	}
	// A contended resource is not a tool error in the agent loop — it parks the
	// task. There is no park available mid-session here, so it comes back as an
	// error the model can act on: try again later, or work on something else.
	if result.ResourceBlock != nil {
		detail := strings.TrimSpace(result.ResourceBlock.Detail)
		if detail == "" {
			detail = "the resource is held by another run"
		}
		return textResult(fmt.Sprintf("%s is waiting on %s: %s", name, result.ResourceBlock.Resource, detail), true)
	}

	blocks := make([]content, 0, len(result.Images)+1)
	text := result.Content
	if strings.TrimSpace(text) == "" && len(result.Images) == 0 {
		// Same rule the agent loop applies to an empty tool message: a blank
		// result reads as a broken tool.
		text = fmt.Sprintf("%s returned no output.", name)
	}
	if text != "" {
		blocks = append(blocks, content{Type: "text", Text: text})
	}
	for _, img := range result.Images {
		if img.Data == "" {
			continue
		}
		mime := img.MediaType
		if mime == "" {
			// The tools that produce images all set it; a missing one is still
			// better sent as PNG than as a block the client refuses to render.
			mime = "image/png"
		}
		blocks = append(blocks, content{Type: "image", Data: img.Data, MimeType: mime})
	}
	return callToolResult{Content: blocks, IsError: result.IsError}
}

// callID is the ToolCallID a call made over MCP is executed under.
//
// The registry stamps it onto the result and the audit decorator logs it, so it
// has to be unique per call — but unlike a loop run there is no model-assigned
// id to reuse, and the JSON-RPC request id is only unique within one client
// session. The prefix is there so an audit row from a CLI session is
// distinguishable from one the loop produced.
func callID() string {
	return "mcp-" + uuid.NewString()
}
