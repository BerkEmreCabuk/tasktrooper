package code

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const writeFileToolName = "write_file"

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type writeFileTool struct{}

// NewWriteFileTool builds the workspace file writer.
//
// Creating a file meant a shell heredoc, and source code is the worst possible
// heredoc payload: backticks and $( ) are command substitution, quotes have to
// survive two levels of escaping, and the sandbox's allowlist mode rejects the
// whole command for containing them — which is how an agent ended up reporting
// "I do not have permission to create files" while holding the shell.
func NewWriteFileTool() port.ToolExecutor {
	return &writeFileTool{}
}

func (t *writeFileTool) Name() string {
	return writeFileToolName
}

func (t *writeFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: writeFileToolName,
			Description: "Create a workspace file, or replace one whole. Parent directories are created as needed. " +
				"This is how you write a new file — not a shell heredoc, which mangles backticks, quotes and template literals. " +
				"To change part of an existing file use edit_file or edit_lines instead; this overwrites everything.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path of the file to write, relative to the workspace root",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Full contents of the file. Written verbatim; a trailing newline is added when missing.",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func (t *writeFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args writeFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(writeFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Path) == "" {
		return toolError(writeFileToolName, "path is required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(writeFileToolName, err.Error())
	}
	abs, err := resolveEditablePath(root, args.Path, writeFileToolName)
	if err != nil {
		return toolError(writeFileToolName, err.Error())
	}

	existed := false
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(abs); statErr == nil {
		if info.IsDir() {
			return toolError(writeFileToolName, fmt.Sprintf("%s is a directory, not a file.", args.Path))
		}
		existed = true
		mode = info.Mode().Perm()
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return toolError(writeFileToolName, fmt.Sprintf("create parent directory for %s: %v", args.Path, err))
	}

	content := args.Content
	// A source file without a trailing newline trips linters, diff tools and
	// "\ No newline at end of file" noise in every later review.
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(abs, []byte(content), mode); err != nil {
		return toolError(writeFileToolName, fmt.Sprintf("write %s: %v", args.Path, err))
	}

	verb := "created"
	if existed {
		verb = "overwritten"
	}
	return domain.ToolResult{
		Name: writeFileToolName,
		Content: fmt.Sprintf("%s: %s, %d lines, %d bytes. The file is on disk — do not read it back to check.",
			args.Path, verb, len(splitLines(content)), len(content)),
	}
}
