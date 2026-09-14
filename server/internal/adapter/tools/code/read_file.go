package code

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const readFileToolName = "read_file"

// defaultReadFileLines is how much of a file one call hands back when the
// caller does not ask for a window. It is deliberately large: the deployment
// had no read tool at all, so agents read source through
// `run_terminal: sed -n '630,640p'` and slid the window ten lines at a time —
// one LLM round-trip, one tool execution and one full context replay for every
// ten lines of a 1200-line file. A run spent sixty of its eighty iterations
// scrolling and never reached the edit it was dispatched to make.
const defaultReadFileLines = 800

// maxReadFileChars keeps one call's payload under the loop's tool-output cap
// (tools.max_tool_output_chars, 16000 by default). Staying below it matters:
// the loop truncates the MIDDLE of an oversized result, which in a file listing
// silently removes lines the model then believes it has read.
const maxReadFileChars = 12000

// maxReadFileBytes refuses a file too large to make sense of through this tool
// at all, rather than reading a gigabyte into the pod's memory to show 800
// lines of it.
const maxReadFileBytes = 20 << 20

type readFileArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

type readFileTool struct{}

// NewReadFileTool builds the workspace file reader. It takes no toolkit: unlike
// the search tools it needs no semantic index, only the workspace root the
// context already carries, so it stays available on repositories that have
// never been indexed.
func NewReadFileTool() port.ToolExecutor {
	return &readFileTool{}
}

func (t *readFileTool) Name() string {
	return readFileToolName
}

func (t *readFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: readFileToolName,
			Description: fmt.Sprintf(
				"Read a workspace file with line numbers. Returns up to %d lines per call and tells you the file's total line count, "+
					"so one call is normally the whole file. Prefer this over reading files through the shell — "+
					"`cat`, `sed -n`, `head` and `tail` cost an entire agent turn per window and lose the line numbers you need to edit by.",
				defaultReadFileLines),
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path of the file to read, relative to the workspace root",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "First line to return, 1-based (default 1). Only needed for a file too long to return in one call.",
					},
					"limit": map[string]interface{}{
						"type": "integer",
						"description": fmt.Sprintf(
							"How many lines to return (default %d). Do not lower it to page through a file in small windows — read it in one call.",
							defaultReadFileLines),
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *readFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args readFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(readFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Path) == "" {
		return toolError(readFileToolName, "path is required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(readFileToolName, err.Error())
	}

	// Same containment rule as grep_code: `path` is model output, and
	// filepath.Join cleans rather than confines, so "../../etc/passwd" would
	// otherwise resolve to a readable absolute path outside the workspace.
	abs, err := workspace.ResolveWithinRoot(root, args.Path)
	if err != nil {
		return toolError(readFileToolName, err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return toolError(readFileToolName, fmt.Sprintf(
				"no such file: %s. Paths are relative to the workspace root — use get_repo_tree or grep_code to find the real path instead of guessing another one.",
				args.Path))
		}
		return toolError(readFileToolName, fmt.Sprintf("stat %s: %v", args.Path, err))
	}
	if info.IsDir() {
		return toolError(readFileToolName, fmt.Sprintf(
			"%s is a directory, not a file. Use get_repo_tree to list what is inside it.", args.Path))
	}
	if info.Size() > maxReadFileBytes {
		return toolError(readFileToolName, fmt.Sprintf(
			"%s is %d bytes, too large to read. Search it with grep_code instead.", args.Path, info.Size()))
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return toolError(readFileToolName, fmt.Sprintf("read %s: %v", args.Path, err))
	}
	// A binary file rendered as text is thousands of tokens of noise that also
	// costs the run a UTF-8 validity error further down the wire.
	if !utf8.Valid(raw) {
		return toolError(readFileToolName, fmt.Sprintf(
			"%s is not a text file (%d bytes of binary content).", args.Path, len(raw)))
	}

	return domain.ToolResult{
		Name:    readFileToolName,
		Content: renderFileWindow(args.Path, string(raw), args.Offset, args.Limit),
	}
}

// renderFileWindow numbers the requested slice of a file and says where it sits
// in the whole, so the model knows whether it has read everything without
// having to probe for the end with another call.
func renderFileWindow(displayPath, content string, offset, limit int) string {
	lines := splitLines(content)
	total := len(lines)

	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 {
		limit = defaultReadFileLines
	}
	if limit > defaultReadFileLines {
		limit = defaultReadFileLines
	}

	if total == 0 {
		return fmt.Sprintf("%s is empty (0 lines).", displayPath)
	}
	if offset > total {
		return fmt.Sprintf("%s has %d lines; offset %d is past the end.", displayPath, total, offset)
	}

	start := offset - 1
	end := min(start+limit, total)

	var body strings.Builder
	last := start
	truncated := false
	for i := start; i < end; i++ {
		line := fmt.Sprintf("%6d→%s\n", i+1, lines[i])
		// Char cap beats the line cap: a minified bundle is one line of 400k
		// characters, and the loop would cut the middle out of it without
		// saying which part it kept.
		if body.Len()+len(line) > maxReadFileChars {
			truncated = true
			break
		}
		body.WriteString(line)
		last = i + 1
	}
	if last == start {
		// Not even one line fit under the cap; hand back a clipped first line
		// rather than an empty result the model reads as "the file is empty".
		clipped := domain.TruncateHead(lines[start], maxReadFileChars)
		body.WriteString(fmt.Sprintf("%6d→%s…\n", start+1, clipped))
		last = start + 1
		truncated = true
	}

	header := fmt.Sprintf("%s — lines %d-%d of %d\n", displayPath, start+1, last, total)
	footer := ""
	switch {
	case last < total && truncated:
		footer = fmt.Sprintf("\n[stopped at line %d: this call's character budget is full. Continue with read_file offset=%d.]", last, last+1)
	case last < total:
		footer = fmt.Sprintf("\n[%d more lines. Continue with read_file offset=%d.]", total-last, last+1)
	default:
		footer = "\n[end of file]"
	}
	return header + body.String() + footer
}

// splitLines splits on \n and drops the empty element a trailing newline
// produces, so a 10-line file reports 10 lines rather than 11.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
