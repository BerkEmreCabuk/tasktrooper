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

const editLinesToolName = "edit_lines"

// contextLines is how much of the file around a change comes back with the
// result. It exists to end the edit → read-it-again round-trip: the model can
// see what it just produced, in place, without spending another agent turn.
const contextLines = 4

const (
	modeReplace     = "replace"
	modeInsertAfter = "insert_after"
	modeDelete      = "delete"
)

type editLinesArgs struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Text      string `json:"text"`
}

type editLinesTool struct{}

// NewEditLinesTool builds the line-oriented editor.
//
// edit_file covers a rename or a removal, but not everything an edit is: adding
// a function, inserting an import, deleting a block. Expressing those as a
// string replacement means echoing back a chunk of the file exactly, which is
// where a small model spends its turns failing on whitespace. read_file already
// hands out line numbers — this takes them straight back.
func NewEditLinesTool() port.ToolExecutor {
	return &editLinesTool{}
}

func (t *editLinesTool) Name() string {
	return editLinesToolName
}

func (t *editLinesTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: editLinesToolName,
			Description: "Change a workspace file by line number: replace a range of lines, insert new lines after a line, or delete a range. " +
				"Use it with the line numbers read_file gives you — this is how you add a function, insert an import or remove a block, " +
				"where edit_file would mean echoing back the existing text exactly. " +
				"The result shows the changed region as it now stands, so you never need to re-read the file to check. " +
				"Line numbers refer to the file as it is NOW: after an edit that adds or removes lines, work bottom-up or read the file again.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path of the file to edit, relative to the workspace root",
					},
					"mode": map[string]interface{}{
						"type": "string",
						"enum": []string{modeReplace, modeInsertAfter, modeDelete},
						"description": "replace: text takes the place of lines start_line..end_line. " +
							"insert_after: text is inserted after start_line (use 0 to insert at the top of the file). " +
							"delete: lines start_line..end_line are removed.",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "First line of the range, 1-based. For insert_after, the line the new text goes after (0 = top of file).",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Last line of the range, 1-based and inclusive. Defaults to start_line. Ignored for insert_after.",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The new lines, for replace and insert_after. Written as-is; no trailing newline needed.",
					},
				},
				"required": []string{"path", "mode", "start_line"},
			},
		},
	}
}

func (t *editLinesTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args editLinesArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(editLinesToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Path) == "" {
		return toolError(editLinesToolName, "path is required")
	}
	mode := strings.ToLower(strings.TrimSpace(args.Mode))
	switch mode {
	case modeReplace, modeInsertAfter, modeDelete:
	case "":
		return toolError(editLinesToolName, "mode is required: replace, insert_after or delete")
	default:
		return toolError(editLinesToolName, fmt.Sprintf(
			"unknown mode %q. Use replace, insert_after or delete.", args.Mode))
	}
	if mode != modeDelete && args.Text == "" && mode == modeReplace {
		return toolError(editLinesToolName, "text is required for replace. To remove lines, use mode delete.")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(editLinesToolName, err.Error())
	}
	abs, err := resolveEditablePath(root, args.Path, editLinesToolName)
	if err != nil {
		return toolError(editLinesToolName, err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return toolError(editLinesToolName, fmt.Sprintf(
				"no such file: %s. Use get_repo_tree or grep_code to find it, or write_file to create it.", args.Path))
		}
		return toolError(editLinesToolName, fmt.Sprintf("stat %s: %v", args.Path, err))
	}
	if info.IsDir() {
		return toolError(editLinesToolName, fmt.Sprintf("%s is a directory, not a file.", args.Path))
	}
	if info.Size() > maxEditFileBytes {
		return toolError(editLinesToolName, fmt.Sprintf(
			"%s is %d bytes, too large to edit through this tool.", args.Path, info.Size()))
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return toolError(editLinesToolName, fmt.Sprintf("read %s: %v", args.Path, err))
	}
	if !utf8.Valid(raw) {
		return toolError(editLinesToolName, fmt.Sprintf("%s is not a text file.", args.Path))
	}

	original := splitLines(string(raw))
	updated, from, to, err := applyLineEdit(original, mode, args.StartLine, args.EndLine, args.Text)
	if err != nil {
		return toolError(editLinesToolName, fmt.Sprintf("%s: %v", args.Path, err))
	}

	out := strings.Join(updated, "\n")
	if len(updated) > 0 {
		out += "\n"
	}
	if err := os.WriteFile(abs, []byte(out), info.Mode().Perm()); err != nil {
		return toolError(editLinesToolName, fmt.Sprintf("write %s: %v", args.Path, err))
	}

	return domain.ToolResult{
		Name:    editLinesToolName,
		Content: lineEditSummary(args.Path, mode, len(original), updated, from, to),
	}
}

// applyLineEdit returns the new file and the 1-based range the change now
// occupies (from > to means the edit removed lines and left nothing there).
func applyLineEdit(lines []string, mode string, start, end int, text string) ([]string, int, int, error) {
	total := len(lines)

	if mode == modeInsertAfter {
		if start < 0 || start > total {
			return nil, 0, 0, fmt.Errorf("start_line %d is outside the file, which has %d lines (use 0 to insert at the top)", start, total)
		}
		inserted := splitLines(text + "\n")
		out := make([]string, 0, total+len(inserted))
		out = append(out, lines[:start]...)
		out = append(out, inserted...)
		out = append(out, lines[start:]...)
		return out, start + 1, start + len(inserted), nil
	}

	if start < 1 {
		return nil, 0, 0, fmt.Errorf("start_line must be 1 or greater, got %d", start)
	}
	if start > total {
		return nil, 0, 0, fmt.Errorf("start_line %d is past the end; the file has %d lines", start, total)
	}
	if end <= 0 {
		end = start
	}
	if end < start {
		return nil, 0, 0, fmt.Errorf("end_line %d is before start_line %d", end, start)
	}
	if end > total {
		return nil, 0, 0, fmt.Errorf("end_line %d is past the end; the file has %d lines", end, total)
	}

	tail := lines[end:]
	out := make([]string, 0, total)
	out = append(out, lines[:start-1]...)
	if mode == modeDelete {
		out = append(out, tail...)
		return out, start, start - 1, nil
	}
	replacement := splitLines(text + "\n")
	out = append(out, replacement...)
	out = append(out, tail...)
	return out, start, start + len(replacement) - 1, nil
}

// lineEditSummary states what happened and shows the region as it now stands.
// Both halves matter: the count is the proof the edit landed, and the rendered
// window is what stops the model spending a turn reading the file back.
func lineEditSummary(path, mode string, before int, updated []string, from, to int) string {
	after := len(updated)
	var what string
	switch mode {
	case modeInsertAfter:
		what = fmt.Sprintf("inserted %d line(s)", to-from+1)
	case modeDelete:
		what = fmt.Sprintf("deleted %d line(s)", before-after)
	default:
		what = fmt.Sprintf("replaced with %d line(s)", to-from+1)
	}

	head := fmt.Sprintf("%s: %s. File is now %d lines (was %d).\n", path, what, after, before)

	windowStart := max(from-contextLines, 1)
	windowEnd := min(to+contextLines, after)
	if to < from {
		// A delete leaves nothing at the range; show where it used to be.
		windowEnd = min(from+contextLines, after)
	}
	if windowStart > after {
		return head + "[the file is now empty]"
	}

	var body strings.Builder
	body.WriteString("Now reads:\n")
	for i := windowStart; i <= windowEnd; i++ {
		body.WriteString(fmt.Sprintf("%6d→%s\n", i, updated[i-1]))
	}
	return head + body.String() + "[the edit is on disk — do not read the file again to check]"
}
