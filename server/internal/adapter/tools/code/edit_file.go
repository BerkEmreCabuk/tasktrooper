package code

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const editFileToolName = "edit_file"

// maxEditFileBytes is the largest file this tool will rewrite. A whole-file
// read-modify-write is the wrong shape for anything bigger, and a generated
// bundle is not something an agent should be editing by hand.
const maxEditFileBytes = 20 << 20

// reportedLineLimit bounds how many changed line numbers the result names. The
// count is always exact; past this the list would be noise in the context.
const reportedLineLimit = 20

type editFileArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type editFileTool struct{}

// NewEditFileTool builds the workspace file editor.
//
// Nothing here could write a file except run_terminal, so every change went out
// as `sed -i` — one file, one pattern, per agent turn. Removing a symbol from
// four files cost eight iterations: the edit, then a grep to find out whether it
// had landed, because a silent `sed -i` reports nothing about what it matched.
// This tool answers that question in the same call, which is what collapses the
// verify round-trip.
func NewEditFileTool() port.ToolExecutor {
	return &editFileTool{}
}

func (t *editFileTool) Name() string {
	return editFileToolName
}

func (t *editFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: editFileToolName,
			Description: "Replace an exact string in a workspace file. This is how you change code — not `sed -i` through the shell. " +
				"The result says how many occurrences changed and on which lines, so you never need to grep afterwards to check whether the edit landed. " +
				"Use replace_all to change every occurrence in the file in one call.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path of the file to edit, relative to the workspace root",
					},
					"old_string": map[string]interface{}{
						"type": "string",
						"description": "Exact text to replace, copied from read_file output without the line-number prefix. " +
							"Must match a single place in the file unless replace_all is set — include surrounding lines to make it unique.",
					},
					"new_string": map[string]interface{}{
						"type":        "string",
						"description": "Replacement text. Pass an empty string to delete the matched text.",
					},
					"replace_all": map[string]interface{}{
						"type": "boolean",
						"description": "Replace every occurrence in the file rather than requiring a unique match. " +
							"This is how you remove or rename a symbol across a file in one call instead of one edit per occurrence.",
					},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
		},
	}
}

func (t *editFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args editFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(editFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Path) == "" {
		return toolError(editFileToolName, "path is required")
	}
	if args.OldString == "" {
		return toolError(editFileToolName, "old_string is required. To create a file or replace it whole, use write_file.")
	}
	if args.OldString == args.NewString {
		return toolError(editFileToolName, "old_string and new_string are identical — this edit would change nothing.")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(editFileToolName, err.Error())
	}
	abs, err := resolveEditablePath(root, args.Path, editFileToolName)
	if err != nil {
		return toolError(editFileToolName, err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return toolError(editFileToolName, fmt.Sprintf(
				"no such file: %s. Paths are relative to the workspace root — use get_repo_tree or grep_code to find it, or write_file to create it.",
				args.Path))
		}
		return toolError(editFileToolName, fmt.Sprintf("stat %s: %v", args.Path, err))
	}
	if info.IsDir() {
		return toolError(editFileToolName, fmt.Sprintf("%s is a directory, not a file.", args.Path))
	}
	if info.Size() > maxEditFileBytes {
		return toolError(editFileToolName, fmt.Sprintf(
			"%s is %d bytes, too large to edit through this tool.", args.Path, info.Size()))
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return toolError(editFileToolName, fmt.Sprintf("read %s: %v", args.Path, err))
	}
	if !utf8.Valid(raw) {
		return toolError(editFileToolName, fmt.Sprintf("%s is not a text file.", args.Path))
	}

	content := string(raw)
	count := strings.Count(content, args.OldString)
	switch {
	case count == 0:
		// The single most common failure: the model reconstructs the text from
		// memory instead of copying it, and whitespace or a line-number prefix
		// differs. Say which, rather than letting it retry the same string.
		return toolError(editFileToolName, fmt.Sprintf(
			"old_string was not found in %s. Copy it exactly from read_file output — without the line-number prefix, "+
				"and with the file's own indentation and line breaks.", args.Path))
	case count > 1 && !args.ReplaceAll:
		return toolError(editFileToolName, fmt.Sprintf(
			"old_string matches %d places in %s. Either pass replace_all:true to change all %d, "+
				"or include the surrounding lines so it matches exactly one.", count, args.Path, count))
	}

	replacements := 1
	if args.ReplaceAll {
		replacements = count
	}
	lines := changedLineNumbers(content, args.OldString, args.ReplaceAll)
	updated := strings.Replace(content, args.OldString, args.NewString, replacements)

	// Preserve the file's mode: a build script or a hook that loses its
	// executable bit fails later, somewhere that never mentions this edit.
	if err := os.WriteFile(abs, []byte(updated), info.Mode().Perm()); err != nil {
		return toolError(editFileToolName, fmt.Sprintf("write %s: %v", args.Path, err))
	}

	return domain.ToolResult{
		Name:    editFileToolName,
		Content: editSummary(args.Path, replacements, lines),
	}
}

// editSummary is what makes the follow-up grep unnecessary: it states the count
// and where, so "did it land?" is already answered.
func editSummary(path string, replacements int, lines []int) string {
	occurrence := "occurrence"
	if replacements != 1 {
		occurrence = "occurrences"
	}
	summary := fmt.Sprintf("%s: %d %s replaced", path, replacements, occurrence)
	if len(lines) == 0 {
		return summary + "."
	}
	shown := lines
	suffix := ""
	if len(shown) > reportedLineLimit {
		shown = shown[:reportedLineLimit]
		suffix = fmt.Sprintf(" and %d more", len(lines)-reportedLineLimit)
	}
	parts := make([]string, 0, len(shown))
	for _, n := range shown {
		parts = append(parts, fmt.Sprint(n))
	}
	return fmt.Sprintf("%s at line %s%s. The edit is on disk — do not grep to verify it.",
		summary, strings.Join(parts, ", "), suffix)
}

// changedLineNumbers reports the 1-based lines where the match starts, counted
// against the file as it was before the edit.
func changedLineNumbers(content, old string, all bool) []int {
	var out []int
	offset := 0
	for {
		idx := strings.Index(content[offset:], old)
		if idx < 0 {
			break
		}
		at := offset + idx
		out = append(out, strings.Count(content[:at], "\n")+1)
		if !all {
			break
		}
		offset = at + len(old)
		if offset >= len(content) {
			break
		}
	}
	return out
}
